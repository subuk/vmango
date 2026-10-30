import { writeFileSync } from "node:fs";
// Prevent Go's recursive package discovery from entering npm dependencies.
writeFileSync(
    new URL("../node_modules/go.mod", import.meta.url),
    "module vmango-ui-dependencies\n",
);
