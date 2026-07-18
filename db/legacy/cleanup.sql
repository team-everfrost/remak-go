\set ON_ERROR_STOP on

-- Run only after the migrated service and S3 artifacts have been accepted.
DROP SCHEMA legacy_migration CASCADE;

