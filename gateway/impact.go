package main

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type ImpactAnalysis struct {
	Status                string   `json:"status"`
	Reason                string   `json:"reason"`
	EstimatedAffectedRows int64    `json:"estimated_affected_rows"`
	EstimatedTableRows    int64    `json:"estimated_table_rows"`
	AffectedPercent       float64  `json:"affected_percent"`
	ProtectedTable        bool     `json:"protected_table"`
	ProtectedColumns      []string `json:"protected_columns"`
	ImpactCritical        bool     `json:"impact_critical"`
}

func (s *MCPServer) analyzeImpact(ctx context.Context, args executeQueryParams, analysis sqlAnalysis) *ImpactAnalysis {
	if !s.policy.ImpactAnalysisEnabled || (analysis.Operation != "DELETE" && analysis.Operation != "UPDATE") {
		return nil
	}
	impact := &ImpactAnalysis{Status: "skipped"}
	table := strings.TrimSpace(analysis.Table)
	if table == "" || strings.TrimSpace(args.Query) == "" {
		return s.unknownImpact("impact analysis requires a single target table")
	}

	impact.ProtectedTable = containsFold(s.policy.ProtectedTables, table)
	if analysis.Operation == "UPDATE" {
		impact.ProtectedColumns = protectedUpdatedColumns(args.Query, s.policy.ProtectedColumns[table])
	}
	if impact.ProtectedTable || len(impact.ProtectedColumns) > 0 {
		impact.Status = "protected_object"
		impact.ImpactCritical = true
		impact.Reason = "write touches protected table or column"
		return impact
	}

	whereClause := extractWhereClause(args.Query)
	if whereClause == "" {
		return s.unknownImpact("write predicate could not be extracted")
	}

	db, closeDB, err := s.openQueryDB(args)
	if err != nil {
		return s.unknownImpact(err.Error())
	}
	if closeDB != nil {
		defer closeDB()
	}

	impactCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	qualified := quoteQualified(analysis.Schema, table)
	countQuery := fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", qualified, whereClause)
	if err := db.QueryRowContext(impactCtx, countQuery).Scan(&impact.EstimatedAffectedRows); err != nil {
		return s.unknownImpact("affected row count failed: " + err.Error())
	}
	totalQuery := fmt.Sprintf("SELECT count(*) FROM %s", qualified)
	if err := db.QueryRowContext(impactCtx, totalQuery).Scan(&impact.EstimatedTableRows); err != nil {
		return s.unknownImpact("table row count failed: " + err.Error())
	}
	if impact.EstimatedTableRows > 0 {
		impact.AffectedPercent = (float64(impact.EstimatedAffectedRows) / float64(impact.EstimatedTableRows)) * 100
	}
	impact.Status = "estimated"
	impact.Reason = "write impact is below configured critical thresholds"
	if impact.EstimatedAffectedRows > s.policy.MaxWriteRowsWithoutCritical || impact.AffectedPercent > s.policy.MaxWritePercentWithoutCritical {
		impact.ImpactCritical = true
		impact.Reason = "write impact exceeds configured thresholds"
	}
	return impact
}

func (s *MCPServer) unknownImpact(reason string) *ImpactAnalysis {
	impact := &ImpactAnalysis{Status: "unknown", Reason: reason}
	if strings.EqualFold(s.policy.UnknownImpactAction, PolicyActionBlock) || strings.EqualFold(s.policy.UnknownImpactAction, PolicyActionApprove) {
		impact.ImpactCritical = true
	}
	return impact
}

func (s *MCPServer) openQueryDB(args executeQueryParams) (*sql.DB, func() error, error) {
	if s.db != nil {
		return s.db, nil, nil
	}
	if strings.TrimSpace(args.DBURL) == "" {
		return nil, nil, fmt.Errorf("impact analysis requires gateway --db or db_url")
	}
	db, err := sql.Open("postgres", args.DBURL)
	if err != nil {
		return nil, nil, err
	}
	return db, db.Close, nil
}

func extractWhereClause(query string) string {
	re := regexp.MustCompile(`(?is)\bwhere\b(.+)$`)
	match := re.FindStringSubmatch(query)
	if len(match) != 2 {
		return ""
	}
	clause := strings.TrimSpace(match[1])
	clause = strings.TrimSuffix(clause, ";")
	if strings.Contains(clause, ";") {
		return ""
	}
	return clause
}

func protectedUpdatedColumns(query string, protected []string) []string {
	if len(protected) == 0 {
		return nil
	}
	re := regexp.MustCompile(`(?is)\bset\b(.+?)\bwhere\b`)
	match := re.FindStringSubmatch(query)
	if len(match) != 2 {
		return nil
	}
	assignments := strings.Split(match[1], ",")
	var touched []string
	for _, assignment := range assignments {
		name := strings.TrimSpace(strings.SplitN(assignment, "=", 2)[0])
		name = strings.Trim(name, `"`)
		if containsFold(protected, name) {
			touched = append(touched, name)
		}
	}
	return touched
}

func quoteQualified(schema, table string) string {
	if strings.TrimSpace(schema) == "" {
		return quoteIdent(table)
	}
	return quoteIdent(schema) + "." + quoteIdent(table)
}

func quoteIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
