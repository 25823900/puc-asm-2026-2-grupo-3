-- Executado pelo PostgreSQL apenas na primeira subida, com o volume de dados vazio.
-- Para aplicar uma alteração feita aqui, recrie o volume: docker compose down -v

\getenv links_password LINKS_DB_PASSWORD
\getenv stats_password STATS_DB_PASSWORD

CREATE USER links_user WITH PASSWORD :'links_password';
CREATE DATABASE links OWNER links_user;

CREATE USER stats_user WITH PASSWORD :'stats_password';
CREATE DATABASE stats OWNER stats_user;
