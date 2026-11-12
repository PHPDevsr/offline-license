-- Apply in the dedicated License Server database as a database administrator.
CREATE TABLE license_products (
    product VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    PRIMARY KEY (product)
) ENGINE=InnoDB;

CREATE TABLE licenses (
    license_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    installation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    product VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    expired DATETIME(6) NOT NULL COMMENT 'UTC',
    expired_extra_gracia DATETIME(6) NOT NULL COMMENT 'UTC',
    PRIMARY KEY (license_id),
    CONSTRAINT fk_licenses_product FOREIGN KEY (product) REFERENCES license_products (product)
) ENGINE=InnoDB;
