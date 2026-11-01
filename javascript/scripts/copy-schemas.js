'use strict';

const fs = require('fs');
const path = require('path');

const from = path.join(__dirname, '..', '..', 'schemas');
const to = path.join(__dirname, '..', 'schemas');

fs.mkdirSync(to, { recursive: true });
for (const file of fs.readdirSync(from).filter((name) => /-v\d+\.\d+\.schema\.json$/.test(name))) {
  fs.copyFileSync(path.join(from, file), path.join(to, file));
}
