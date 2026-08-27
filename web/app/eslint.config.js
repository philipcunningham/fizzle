// Flat ESLint config: strict TypeScript, React hooks, accessibility,
// and the one-way layer boundaries from the engineering framing.
// Layers, inner to outer: boundary, then core / queries / viewstate,
// then ui, then screens, then shell. Imports only point inward.
import boundaries from "eslint-plugin-boundaries";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist/", "node_modules/", "scripts/", "src/core/generated/"] },
  ...tseslint.configs.strictTypeChecked,
  {
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
  },
  {
    files: ["**/*.ts", "**/*.tsx"],
    plugins: {
      "react-hooks": reactHooks,
      "jsx-a11y": jsxA11y,
      boundaries,
    },
    settings: {
      "boundaries/elements": [
        { type: "boundary", pattern: "src/boundary/**" },
        { type: "core", pattern: "src/core/**" },
        { type: "queries", pattern: "src/queries/**" },
        { type: "viewstate", pattern: "src/viewstate/**" },
        { type: "ui", pattern: "src/ui/**" },
        { type: "screens", pattern: "src/screens/**" },
        { type: "dialogs", pattern: "src/dialogs/**" },
        { type: "shell", pattern: "src/shell/**" },
        { type: "tests", pattern: "tests/**" },
      ],
    },
    rules: {
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "error",
      ...jsxA11y.configs.recommended.rules,
      "@typescript-eslint/restrict-template-expressions": ["error", { allowNumber: true }],
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],
      "boundaries/dependencies": [
        "error",
        {
          default: "disallow",
          policies: [
            {
              from: { element: { type: "core" } },
              allow: { to: { element: { type: "boundary" } } },
            },
            {
              from: { element: { type: "queries" } },
              allow: { to: { element: { type: "boundary" } } },
            },
            {
              from: { element: { type: "viewstate" } },
              allow: { to: { element: { type: "boundary" } } },
            },
            {
              from: { element: { type: "ui" } },
              allow: {
                to: { element: { types: { anyOf: ["boundary", "queries", "viewstate"] } } },
              },
            },
            {
              from: { element: { type: "screens" } },
              allow: {
                to: {
                  element: { types: { anyOf: ["boundary", "queries", "viewstate", "ui"] } },
                },
              },
            },
            {
              from: { element: { type: "dialogs" } },
              allow: {
                to: {
                  element: { types: { anyOf: ["boundary", "queries", "viewstate", "ui"] } },
                },
              },
            },
            {
              from: { element: { type: "shell" } },
              allow: {
                to: {
                  element: {
                    types: {
                      anyOf: [
                        "boundary",
                        "core",
                        "queries",
                        "viewstate",
                        "ui",
                        "screens",
                        "dialogs",
                      ],
                    },
                  },
                },
              },
            },
            {
              from: { element: { type: "tests" } },
              allow: {
                to: {
                  element: {
                    types: {
                      anyOf: [
                        "boundary",
                        "core",
                        "queries",
                        "viewstate",
                        "ui",
                        "screens",
                        "dialogs",
                        "shell",
                      ],
                    },
                  },
                },
              },
            },
          ],
        },
      ],
    },
  },
  {
    files: ["*.config.ts", "*.config.js"],
    extends: [tseslint.configs.disableTypeChecked],
  },
);
