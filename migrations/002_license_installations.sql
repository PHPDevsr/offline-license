-- Run after 001 with the License Server stopped and administrative writes paused.
CREATE TABLE license_installations (
    installation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    license_no BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (installation_id),
    KEY ix_license_installations_license_no (license_no),
    CONSTRAINT fk_license_installations_license_no
        FOREIGN KEY (license_no) REFERENCES licenses (license_no)
) ENGINE=InnoDB;

INSERT INTO license_installations (installation_id, license_no)
SELECT installation_id, license_no FROM licenses;

ALTER TABLE licenses
    ADD COLUMN license_key_hash BINARY(32) NULL,
    ADD UNIQUE KEY uq_licenses_license_key_hash (license_key_hash),
    DROP INDEX uq_licenses_installation_id,
    DROP COLUMN installation_id;
