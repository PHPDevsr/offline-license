-- Apply once to an empty License Server database as a database administrator.
CREATE TABLE license_products (
    product_no BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    product_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    product_name VARCHAR(255) NOT NULL,
    PRIMARY KEY (product_no),
    UNIQUE KEY uq_license_products_product_id (product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE licenses (
    license_no BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    product_no BIGINT UNSIGNED NOT NULL,
    license_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    license_key_hash BINARY(32) NULL,
    expired DATETIME(6) NOT NULL COMMENT 'UTC',
    expired_extra_gracia DATETIME(6) NOT NULL COMMENT 'UTC',
    PRIMARY KEY (license_no),
    UNIQUE KEY uq_licenses_license_id (license_id),
    UNIQUE KEY uq_licenses_license_key_hash (license_key_hash),
    CONSTRAINT fk_licenses_product_no FOREIGN KEY (product_no) REFERENCES license_products (product_no)
) ENGINE=InnoDB;

CREATE TABLE license_installations (
    installation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    license_no BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (installation_id),
    KEY ix_license_installations_license_no (license_no),
    CONSTRAINT fk_license_installations_license_no
        FOREIGN KEY (license_no) REFERENCES licenses (license_no)
) ENGINE=InnoDB;
