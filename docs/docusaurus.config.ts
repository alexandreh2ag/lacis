import {themes as prismThemes} from 'prism-react-renderer';
import {existsSync} from 'node:fs';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const repo = 'https://github.com/alexandreh2ag/lacis';
const baseUrl = '/lacis/';

// The version menu shows once a tag has versioned the documentation.
const versioned = existsSync(`${__dirname}/versions.json`);

const config: Config = {
  title: 'Lacis',
  tagline: 'Infrastructure inventory and dependency graph',
  favicon: 'img/favicon.ico',
  // Sharper SVG icon for the browsers that support it; the .ico above stays as the fallback.
  headTags: [
    {tagName: 'link', attributes: {rel: 'icon', type: 'image/svg+xml', href: `${baseUrl}img/favicon.svg`}},
  ],

  future: {
    v4: true,
  },

  // GitHub Pages serves a project site under the name of the repository.
  url: 'https://alexandreh2ag.github.io',
  baseUrl,

  organizationName: 'alexandreh2ag',
  projectName: 'lacis',

  onBrokenLinks: 'throw',
  onBrokenAnchors: 'throw',

  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'throw',
      onBrokenMarkdownImages: 'throw',
    },
  },

  themes: ['@docusaurus/theme-mermaid'],

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          routeBasePath: '/docs',
          sidebarPath: './sidebars.ts',
          editUrl: `${repo}/tree/main/docs/`,
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Lacis',
      logo: {
        alt: 'Lacis',
        src: 'img/logo.svg',
        srcDark: 'img/logo-dark.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Documentation',
        },
        ...(versioned ? [{type: 'docsVersionDropdown' as const, position: 'right' as const}] : []),
        {
          href: repo,
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Documentation',
          items: [
            {label: 'Introduction', to: '/docs'},
            {label: 'Installation', to: '/docs/installation'},
            {label: 'Server configuration', to: '/docs/configuration/server'},
          ],
        },
        {
          title: 'More',
          items: [
            {label: 'GitHub', href: repo},
            {label: 'Releases', href: `${repo}/releases`},
            {label: 'Issues', href: `${repo}/issues`},
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Lacis contributors. MIT license. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'ini', 'json'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
