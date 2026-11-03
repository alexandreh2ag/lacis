import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docsSidebar: [
    'intro',
    'installation',
    {
      type: 'category',
      label: 'Configuration',
      items: ['configuration/server', 'configuration/agent'],
    },
    'collectors',
    'linking',
    'permissions',
    'oidc',
    {
      type: 'category',
      label: 'CLI',
      items: ['cli/server', 'cli/agent'],
    },
    'architecture',
  ],
};

export default sidebars;
