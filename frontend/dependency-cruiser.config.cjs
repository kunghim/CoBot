// Import rules of the frontend; docs/rules/architecture.md explains them.
// Violations that already exist are in import-violations.json, which may only
// shrink (scripts/check-imports.mjs).

// Feature folders, highest first. A folder imports only folders below it, so
// folders cannot form a cycle.
const featureOrder = [
  'home', 'settings', 'chats', 'activity', 'tasks', 'conversations', 'questions', 'approvals',
  'workspace', 'teams', 'providers', 'budget', 'avatars', 'memories', 'search',
];

// The app modules that hold shared state; the rest of app/ assembles features.
const appState = ['SelectedBotContext', 'TeamMembersContext', 'mode', 'shell', 'workspacePath', 'workPanelNav', 'WindowChrome', 'useViewportWidth', 'appearance'];

const featureRules = featureOrder.slice(1).map((name, index) => ({
  name: 'feature-order',
  comment: `${name} sits below ${featureOrder.slice(0, index + 1).join(', ')}`,
  severity: 'error',
  from: { path: `^src/features/${name}/` },
  to: { path: `^src/features/(${featureOrder.slice(0, index + 1).join('|')})/` },
}));

module.exports = {
  forbidden: [
    ...featureRules,
    {
      name: 'components-not-features',
      comment: 'Shared components know no feature.',
      severity: 'error',
      from: { path: '^src/components/' },
      to: { path: '^src/features/' },
    },
    {
      name: 'features-use-app-state-only',
      comment: 'A feature takes shared state from app/, not the assembly around it.',
      severity: 'error',
      from: { path: '^src/features/' },
      to: { path: '^src/app/', pathNot: `^src/app/(${appState.join('|')})\\.tsx?$` },
    },
    {
      name: 'no-circular',
      severity: 'error',
      from: {},
      to: { circular: true },
    },
  ],
  options: {
    // Type-only imports count too: a feature does not lean on a higher one even for its types.
    tsPreCompilationDeps: true,
    tsConfig: { fileName: 'tsconfig.json' },
    exclude: { path: '(\\.test\\.tsx?$|^src/gallery/|^src/api/generated/)' },
    doNotFollow: { path: 'node_modules' },
  },
};
