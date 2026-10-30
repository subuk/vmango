import {
    readFileSync,
    readdirSync,
    statSync,
    writeFileSync,
    existsSync,
} from "node:fs";
import { join } from "node:path";

const lock = JSON.parse(readFileSync("package-lock.json", "utf8"));
const sections = [
    "Vmango frontend third-party notices\n\nRuntime noVNC and Swagger UI notices are also retained under vendor/.",
];
for (const [path, entry] of Object.entries(lock.packages).sort(([a], [b]) =>
    a.localeCompare(b),
)) {
    if (!path || entry.dev || !existsSync(path)) continue;
    const metadata = JSON.parse(
        readFileSync(join(path, "package.json"), "utf8"),
    );
    const licenses = readdirSync(path).filter(
        (name) =>
            statSync(join(path, name)).isFile() &&
            /^(licen[sc]e|copying|notice)([._-]|$)/i.test(name),
    );
    sections.push(
        `${metadata.name}@${metadata.version}\nLicense: ${metadata.license || "See package notices"}\n` +
            licenses
                .map(
                    (name) =>
                        `${name}:\n${readFileSync(join(path, name), "utf8")}`,
                )
                .join("\n"),
    );
}
writeFileSync(
    "dist/THIRD-PARTY-LICENSES.txt",
    sections.join("\n\n" + "=".repeat(72) + "\n\n") + "\n",
);
