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
    installation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    expired DATETIME(6) NOT NULL COMMENT 'UTC',
    expired_extra_gracia DATETIME(6) NOT NULL COMMENT 'UTC',
    PRIMARY KEY (license_no),
    UNIQUE KEY uq_licenses_license_id (license_id),
    UNIQUE KEY uq_licenses_installation_id (installation_id),
    CONSTRAINT fk_licenses_product_no FOREIGN KEY (product_no) REFERENCES license_products (product_no)
) ENGINE=InnoDB;
