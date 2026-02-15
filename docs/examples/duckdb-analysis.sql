-- duckdb-analysis.sql — Query logtap captures exported to parquet
--
-- Export first:
--   logtap export ./capture --format parquet --out capture.parquet
--
-- Run with:
--   duckdb < docs/examples/duckdb-analysis.sql
--   duckdb -c "SELECT count(*) FROM 'capture.parquet'"

-- Total lines
SELECT count(*) AS total_lines FROM 'capture.parquet';

-- Lines per service
SELECT
  labels['app'] AS service,
  count(*) AS lines,
  round(count(*) * 100.0 / sum(count(*)) OVER (), 1) AS pct
FROM 'capture.parquet'
GROUP BY 1
ORDER BY 2 DESC;

-- Error lines per minute
SELECT
  date_trunc('minute', ts) AS minute,
  count(*) AS total,
  count(*) FILTER (WHERE msg ILIKE '%error%' OR msg ILIKE '%fail%') AS errors
FROM 'capture.parquet'
GROUP BY 1
ORDER BY 1;

-- Top 20 error messages (normalized)
SELECT
  regexp_replace(
    regexp_replace(
      regexp_replace(msg, '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}', '<UUID>', 'gi'),
      '\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}', '<IP>', 'g'),
    '\d+ms', '<DUR>', 'g') AS pattern,
  count(*) AS occurrences
FROM 'capture.parquet'
WHERE msg ILIKE '%error%' OR msg ILIKE '%fail%' OR msg ILIKE '%timeout%'
GROUP BY 1
ORDER BY 2 DESC
LIMIT 20;

-- Throughput per second (overall)
SELECT
  date_trunc('second', ts) AS second,
  count(*) AS lines_per_sec
FROM 'capture.parquet'
GROUP BY 1
ORDER BY 2 DESC
LIMIT 10;

-- Service-level error rates
SELECT
  labels['app'] AS service,
  count(*) AS total,
  count(*) FILTER (WHERE msg ILIKE '%error%' OR msg ILIKE '%timeout%') AS errors,
  round(count(*) FILTER (WHERE msg ILIKE '%error%' OR msg ILIKE '%timeout%') * 100.0 / count(*), 2) AS error_pct
FROM 'capture.parquet'
GROUP BY 1
ORDER BY 4 DESC;
