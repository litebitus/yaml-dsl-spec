'use strict';

/* The YAML DSL standard's reader of schema URLs on S3, `s3://<bucket>/<key>`. */

const { S3SchemaReader } = require('./lib/s3-schema-reader');

module.exports = { S3SchemaReader };
