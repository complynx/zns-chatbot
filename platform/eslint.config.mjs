import js from '@eslint/js';
import globals from 'globals';
import unicorn from 'eslint-plugin-unicorn';
import promise from 'eslint-plugin-promise';
import security from 'eslint-plugin-security';
import prettier from 'eslint-config-prettier/flat';

export default [
  { ignores: ['node_modules/**', '**/*.local/**', 'test-results/**'] },
  js.configs.recommended,
  unicorn.configs.recommended,
  promise.configs['flat/recommended'],
  security.configs.recommended,
  {
    rules: {
      eqeqeq: ['error', 'always'],
      'no-var': 'error',
      'prefer-const': 'error',
      'no-implicit-coercion': 'error',
      'no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    },
  },
  {
    files: ['*.mjs', 'tests/**/*.mjs', 'scripts/**/*.mjs'],
    languageOptions: { globals: globals.node },
  },
  {
    files: ['internal/sandbox/**/*.js', 'internal/miniapp/**/*.js'],
    languageOptions: { globals: globals.browser },
  },
  // Browser evaluation callbacks execute inside the test page, not in Node.
  { files: ['tests/**/*.mjs'], languageOptions: { globals: globals.browser } },
  prettier,
];
