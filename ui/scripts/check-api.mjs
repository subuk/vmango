import { execFileSync } from "node:child_process";
import { readFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const temporary = mkdtempSync(join(tmpdir(), "vmango-api-"));
try {
    const output = join(temporary, "schema.ts");
    execFileSync(process.execPath, [
        "node_modules/openapi-typescript/bin/cli.js",
        "../api/testdata/openapi.json",
        "--default-non-nullable",
        "false",
        "-o",
        output,
    ]);
    if (!readFileSync(output).equals(readFileSync("src/api/schema.ts")))
        throw new Error("API types drifted: run npm run api:generate");
} finally {
    rmSync(temporary, { recursive: true, force: true });
}
