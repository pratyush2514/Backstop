package main

import (
	"fmt"
	"sort"
	"strings"

	pganalyze "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

type sqlAnalysis struct {
	RiskLevel        string
	Operation        string
	Table            string
	Schema           string
	StatementCount   int
	TableRecoverable bool
	Reason           string
	ParseError       error
	Impact           *ImpactAnalysis
}

func classifySQL(query string) string {
	return analyzeSQL(query).RiskLevel
}

func criticalRecoveryTable(query string) (string, error) {
	analysis := analyzeSQL(query)
	return criticalRecoveryTableFromAnalysis(analysis)
}

func analyzeSQL(query string) sqlAnalysis {
	if strings.TrimSpace(query) == "" {
		return safeAnalysis("EMPTY", "empty SQL is a validation no-op")
	}

	tree, err := pgquery.Parse(query)
	if err != nil {
		return sqlAnalysis{
			RiskLevel:  RiskCritical,
			Operation:  "PARSE_FAILURE",
			Reason:     "PostgreSQL parser rejected SQL; gateway fails closed",
			ParseError: err,
		}
	}

	statements := tree.GetStmts()
	if len(statements) == 0 {
		return safeAnalysis("EMPTY", "no parseable SQL statements")
	}

	var result sqlAnalysis
	resultSet := false
	criticalTables := map[string]struct{}{}
	criticalSchemas := map[string]string{}
	criticalUnrecoverable := false

	for _, raw := range statements {
		statement := analyzeStatement(raw.GetStmt())
		if !resultSet {
			result = statement
			resultSet = true
		} else {
			result = higherRisk(result, statement)
		}
		if statement.RiskLevel == RiskCritical {
			if statement.TableRecoverable && statement.Table != "" {
				criticalTables[statement.Table] = struct{}{}
				if statement.Schema != "" {
					criticalSchemas[statement.Table] = statement.Schema
				}
			} else {
				criticalUnrecoverable = true
			}
		}
	}

	if result.RiskLevel == RiskCritical {
		tables := sortedKeys(criticalTables)
		switch {
		case len(tables) == 1 && !criticalUnrecoverable:
			result.Table = tables[0]
			result.Schema = criticalSchemas[tables[0]]
			result.TableRecoverable = true
		case len(tables) > 1:
			result.Table = strings.Join(tables, ",")
			result.TableRecoverable = false
			result.Reason = "CRITICAL SQL affects multiple tables; require a single table recovery point"
		case criticalUnrecoverable:
			result.Table = ""
			result.TableRecoverable = false
		}
	}

	result.StatementCount = len(statements)
	return result
}

func analyzeStatement(node *pganalyze.Node) sqlAnalysis {
	if node == nil {
		return criticalAnalysis("UNKNOWN", "", "", false, "statement node is missing")
	}

	switch stmt := node.GetNode().(type) {
	case *pganalyze.Node_SelectStmt:
		return safeAnalysis("SELECT", "read-only SELECT")
	case *pganalyze.Node_VariableShowStmt:
		return safeAnalysis("SHOW", "read-only SHOW")
	case *pganalyze.Node_TransactionStmt:
		return analyzeTransaction(stmt.TransactionStmt)
	case *pganalyze.Node_VariableSetStmt:
		return safeAnalysis("SET", "session variable change")
	case *pganalyze.Node_InsertStmt:
		return highRelationAnalysis("INSERT", stmt.InsertStmt.GetRelation(), "INSERT modifies table data")
	case *pganalyze.Node_DeleteStmt:
		schema, table := rangeVarParts(stmt.DeleteStmt.GetRelation())
		if stmt.DeleteStmt.GetWhereClause() == nil {
			return criticalAnalysis("DELETE", schema, table, table != "", "DELETE without WHERE removes all rows")
		}
		return highAnalysis("DELETE", schema, table, "DELETE with WHERE modifies table data")
	case *pganalyze.Node_UpdateStmt:
		schema, table := rangeVarParts(stmt.UpdateStmt.GetRelation())
		if stmt.UpdateStmt.GetWhereClause() == nil {
			return criticalAnalysis("UPDATE", schema, table, table != "", "UPDATE without WHERE modifies all rows")
		}
		return highAnalysis("UPDATE", schema, table, "UPDATE with WHERE modifies table data")
	case *pganalyze.Node_MergeStmt:
		return highRelationAnalysis("MERGE", stmt.MergeStmt.GetRelation(), "MERGE modifies table data")
	case *pganalyze.Node_DropStmt:
		return analyzeDrop(stmt.DropStmt)
	case *pganalyze.Node_DropdbStmt:
		return criticalAnalysis("DROP DATABASE", "", "", false, "DROP DATABASE is not recoverable by table snapshots")
	case *pganalyze.Node_TruncateStmt:
		return analyzeTruncate(stmt.TruncateStmt)
	case *pganalyze.Node_ExplainStmt:
		return analyzeExplain(stmt.ExplainStmt)
	case *pganalyze.Node_CreateStmt,
		*pganalyze.Node_IndexStmt,
		*pganalyze.Node_ViewStmt,
		*pganalyze.Node_CreateSeqStmt,
		*pganalyze.Node_CreateSchemaStmt,
		*pganalyze.Node_CreateTableAsStmt:
		return highAnalysis("DDL", "", "", "DDL changes database objects")
	case *pganalyze.Node_AlterTableStmt,
		*pganalyze.Node_CopyStmt,
		*pganalyze.Node_CallStmt,
		*pganalyze.Node_DoStmt,
		*pganalyze.Node_RefreshMatViewStmt:
		return highAnalysis("WRITE", "", "", "statement may modify database state")
	default:
		return criticalAnalysis(fmt.Sprintf("%T", stmt), "", "", false, "parsed statement kind is not explicitly classified")
	}
}

func analyzeTransaction(stmt *pganalyze.TransactionStmt) sqlAnalysis {
	switch stmt.GetKind() {
	case pganalyze.TransactionStmtKind_TRANS_STMT_BEGIN,
		pganalyze.TransactionStmtKind_TRANS_STMT_START,
		pganalyze.TransactionStmtKind_TRANS_STMT_COMMIT,
		pganalyze.TransactionStmtKind_TRANS_STMT_ROLLBACK,
		pganalyze.TransactionStmtKind_TRANS_STMT_SAVEPOINT,
		pganalyze.TransactionStmtKind_TRANS_STMT_RELEASE,
		pganalyze.TransactionStmtKind_TRANS_STMT_ROLLBACK_TO:
		return safeAnalysis("TRANSACTION", "transaction control statement")
	default:
		return highAnalysis("TRANSACTION", "", "", "prepared transaction statement changes database state")
	}
}

func analyzeDrop(stmt *pganalyze.DropStmt) sqlAnalysis {
	switch stmt.GetRemoveType() {
	case pganalyze.ObjectType_OBJECT_TABLE:
		objects := dropObjectNames(stmt.GetObjects())
		tables := make([]string, 0, len(objects))
		schema := ""
		for _, object := range objects {
			tables = append(tables, object.table)
			if schema == "" {
				schema = object.schema
			}
		}
		if len(tables) != 1 {
			return criticalAnalysis("DROP TABLE", "", strings.Join(tables, ","), false, "DROP TABLE must target exactly one table for sidecar recovery")
		}
		return criticalAnalysis("DROP TABLE", schema, tables[0], true, "DROP TABLE destroys a table")
	case pganalyze.ObjectType_OBJECT_DATABASE:
		return criticalAnalysis("DROP DATABASE", "", "", false, "DROP DATABASE is not recoverable by table snapshots")
	case pganalyze.ObjectType_OBJECT_SCHEMA:
		return criticalAnalysis("DROP SCHEMA", "", "", false, "DROP SCHEMA is not recoverable by table snapshots")
	default:
		return highAnalysis("DROP", "", "", fmt.Sprintf("DROP %s changes database objects", stmt.GetRemoveType()))
	}
}

func analyzeTruncate(stmt *pganalyze.TruncateStmt) sqlAnalysis {
	relations := relationNodeNames(stmt.GetRelations())
	tables := make([]string, 0, len(relations))
	schema := ""
	for _, relation := range relations {
		tables = append(tables, relation.table)
		if schema == "" {
			schema = relation.schema
		}
	}
	if len(tables) != 1 {
		return criticalAnalysis("TRUNCATE", "", strings.Join(tables, ","), false, "TRUNCATE must target exactly one table for sidecar recovery")
	}
	return criticalAnalysis("TRUNCATE", schema, tables[0], true, "TRUNCATE removes all rows from a table")
}

func analyzeExplain(stmt *pganalyze.ExplainStmt) sqlAnalysis {
	if explainAnalyze(stmt.GetOptions()) {
		inner := analyzeStatement(stmt.GetQuery())
		inner.Operation = "EXPLAIN ANALYZE " + inner.Operation
		return inner
	}
	return safeAnalysis("EXPLAIN", "EXPLAIN without ANALYZE does not execute the query")
}

func explainAnalyze(options []*pganalyze.Node) bool {
	for _, option := range options {
		def := option.GetDefElem()
		if def == nil {
			continue
		}
		if strings.EqualFold(def.GetDefname(), "analyze") {
			return true
		}
	}
	return false
}

func criticalRecoveryTableFromAnalysis(analysis sqlAnalysis) (string, error) {
	if analysis.RiskLevel != RiskCritical && analysis.RiskLevel != RiskImpactCritical {
		return "", fmt.Errorf("query is not CRITICAL")
	}
	if !analysis.TableRecoverable || strings.TrimSpace(analysis.Table) == "" {
		if analysis.ParseError != nil {
			return "", fmt.Errorf("%s: %v", analysis.Reason, analysis.ParseError)
		}
		return "", fmt.Errorf("%s", analysis.Reason)
	}
	if strings.Contains(analysis.Table, ",") {
		return "", fmt.Errorf("CRITICAL SQL must target exactly one table for sidecar recovery")
	}
	return analysis.Table, nil
}

func safeAnalysis(operation, reason string) sqlAnalysis {
	return sqlAnalysis{RiskLevel: RiskSafe, Operation: operation, Reason: reason}
}

func highRelationAnalysis(operation string, relation *pganalyze.RangeVar, reason string) sqlAnalysis {
	schema, table := rangeVarParts(relation)
	return highAnalysis(operation, schema, table, reason)
}

func highAnalysis(operation, schema, table, reason string) sqlAnalysis {
	return sqlAnalysis{RiskLevel: RiskHigh, Operation: operation, Schema: schema, Table: table, Reason: reason}
}

func criticalAnalysis(operation, schema, table string, recoverable bool, reason string) sqlAnalysis {
	return sqlAnalysis{
		RiskLevel:        RiskCritical,
		Operation:        operation,
		Schema:           schema,
		Table:            table,
		TableRecoverable: recoverable,
		Reason:           reason,
	}
}

func higherRisk(current, next sqlAnalysis) sqlAnalysis {
	if riskRank(next.RiskLevel) > riskRank(current.RiskLevel) {
		return next
	}
	return current
}

func riskRank(level string) int {
	switch level {
	case RiskCritical:
		return 3
	case RiskImpactCritical:
		return 3
	case RiskHigh:
		return 2
	default:
		return 0
	}
}

type relationName struct {
	schema string
	table  string
}

func relationNodeNames(nodes []*pganalyze.Node) []relationName {
	var names []relationName
	for _, node := range nodes {
		schema, table := rangeVarParts(node.GetRangeVar())
		if table != "" {
			names = append(names, relationName{schema: schema, table: table})
		}
	}
	return names
}

func dropObjectNames(objects []*pganalyze.Node) []relationName {
	var names []relationName
	for _, object := range objects {
		schema, name := objectName(object)
		if name != "" {
			names = append(names, relationName{schema: schema, table: name})
		}
	}
	return names
}

func objectName(node *pganalyze.Node) (string, string) {
	if node == nil {
		return "", ""
	}
	if list := node.GetList(); list != nil {
		items := list.GetItems()
		if len(items) == 0 {
			return "", ""
		}
		last := items[len(items)-1]
		table := ""
		if value := last.GetString_(); value != nil {
			table = value.GetSval()
		}
		schema := ""
		if len(items) > 1 {
			if value := items[len(items)-2].GetString_(); value != nil {
				schema = value.GetSval()
			}
		}
		return schema, table
	}
	if value := node.GetString_(); value != nil {
		return "", value.GetSval()
	}
	if relation := node.GetRangeVar(); relation != nil {
		return rangeVarParts(relation)
	}
	return "", ""
}

func rangeVarName(relation *pganalyze.RangeVar) string {
	_, table := rangeVarParts(relation)
	return table
}

func rangeVarParts(relation *pganalyze.RangeVar) (string, string) {
	if relation == nil {
		return "", ""
	}
	return relation.GetSchemaname(), relation.GetRelname()
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
