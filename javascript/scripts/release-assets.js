'use strict';

const fs = require('fs');
const path = require('path');

const { FAMILIES } = require('..');
const { packagedVersionFiles } = require('../schema-versions');

const [destination] = process.argv.slice(2);
if (!destination) {
  console.error('usage: release-assets.js <folder>');
  process.exit(2);
}
fs.mkdirSync(destination, { recursive: true });
for (const family of Object.values(FAMILIES)) {
  for (const [version, file] of packagedVersionFiles(family)) {
    const asset = `${family}-v${version}.schema.json`;
    fs.copyFileSync(file, path.join(destination, asset));
    console.log(`${asset} from ${path.basename(file)}`);
  }
}
