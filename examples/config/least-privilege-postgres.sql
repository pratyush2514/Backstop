-- Example roles for deploying backstop with AI agents.
-- Replace database names, passwords, schemas, and grants for your application.

CREATE ROLE backstop_agent LOGIN PASSWORD 'replace-with-strong-password';
CREATE ROLE backstop_gateway LOGIN PASSWORD 'replace-with-strong-password';
CREATE ROLE backstop_backup LOGIN PASSWORD 'replace-with-strong-password';

REVOKE ALL ON DATABASE appdb FROM PUBLIC;
GRANT CONNECT ON DATABASE appdb TO backstop_gateway;
GRANT CONNECT ON DATABASE appdb TO backstop_backup;

\connect appdb

REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM backstop_agent;
REVOKE CREATE ON SCHEMA public FROM backstop_gateway;

GRANT USAGE ON SCHEMA public TO backstop_gateway;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO backstop_gateway;
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO backstop_gateway;

GRANT USAGE ON SCHEMA public TO backstop_backup;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO backstop_backup;

-- Do not give AI tools the backstop_gateway password directly. The gateway owns
-- the database connection; agents should receive only BACKSTOP_URL and BACKSTOP_TOKEN.

