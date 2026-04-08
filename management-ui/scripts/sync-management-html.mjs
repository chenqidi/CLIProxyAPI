import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '..');

const sourcePath = path.resolve(projectRoot, 'dist', 'index.html');
const targetPath =
  process.env.CPA_MANAGEMENT_HTML_TARGET?.trim() ||
  path.resolve(projectRoot, '..', 'static', 'management.html');

const syncManagementHtml = async () => {
  try {
    await fs.access(sourcePath);
  } catch {
    throw new Error(`Build output not found: ${sourcePath}`);
  }

  await fs.mkdir(path.dirname(targetPath), { recursive: true });
  await fs.copyFile(sourcePath, targetPath);

  console.log(`Synced management HTML:\n  from: ${sourcePath}\n  to:   ${targetPath}`);
};

await syncManagementHtml();
