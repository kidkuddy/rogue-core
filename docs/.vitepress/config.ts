import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Rogue Core',
  description: 'AI Agent Pipeline Framework',
  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/' },
      { text: 'Architecture', link: '/architecture/' },
      { text: 'Use Cases', link: '/use-cases/' },
    ],
    sidebar: {
      '/guide/': [
        {
          text: 'Getting Started',
          items: [
            { text: 'Introduction', link: '/guide/' },
            { text: 'Quick Start', link: '/guide/quickstart' },
            { text: 'Configuration', link: '/guide/configuration' },
          ],
        },
        {
          text: 'Core Concepts',
          items: [
            { text: 'Pipeline', link: '/guide/pipeline' },
            { text: 'Agents', link: '/guide/agents' },
            { text: 'Powers', link: '/guide/powers' },
            { text: 'ROOT.md', link: '/guide/root-prompt' },
          ],
        },
        {
          text: 'Features',
          items: [
            { text: 'Scheduling', link: '/guide/scheduling' },
            { text: 'Storage', link: '/guide/storage' },
            { text: 'IAM', link: '/guide/iam' },
            { text: 'Telegram', link: '/guide/telegram' },
          ],
        },
      ],
      '/architecture/': [
        {
          text: 'Architecture',
          items: [
            { text: 'Overview', link: '/architecture/' },
            { text: 'Components', link: '/architecture/components' },
            { text: 'Data Flow', link: '/architecture/data-flow' },
          ],
        },
      ],
      '/use-cases/': [
        {
          text: 'Use Cases',
          items: [
            { text: 'Overview', link: '/use-cases/' },
          ],
        },
      ],
    },
    socialLinks: [
      { icon: 'github', link: 'https://github.com/kidkuddy/rogue-core' },
    ],
  },
})
