import type {ReactNode} from 'react';
import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import CodeBlock from '@theme/CodeBlock';
import ThemedImage from '@theme/ThemedImage';

import styles from './index.module.css';

type Feature = {title: string; body: ReactNode};

const features: Feature[] = [
  {
    title: 'An inventory, not monitoring',
    body: (
      <>
        Hosts, services, runtimes, webservers, containers and deployed apps: what runs
        where, with its version and size. Never usage or instant values.
      </>
    ),
  },
  {
    title: 'Dependencies resolved',
    body: (
      <>
        The server links each app to the databases, brokers, caches and APIs its
        configuration points to, through proxies and across hosts, with the proof of
        every link.
      </>
    ),
  },
  {
    title: 'A graph to navigate',
    body: (
      <>
        A projects lens for developers, an infrastructure lens for operations, and an
        impact mode telling what breaks when a service stops.
      </>
    ),
  },
  {
    title: 'No secret leaves the host',
    body: (
      <>
        The agent reads configuration files but sends hosts, ports and resource names
        only: passwords, tokens and command arguments stay on the host.
      </>
    ),
  },
  {
    title: 'Push-only agent',
    body: (
      <>
        One static binary per host, enrolled with a token, sending a snapshot at an
        interval. No inbound port, no SSH access to grant.
      </>
    ),
  },
  {
    title: 'One server binary',
    body: (
      <>
        API, workers and web interface in a single binary backed by PostgreSQL, with
        local accounts or OIDC and fine-grained permissions per namespace.
      </>
    ),
  },
];

const steps = [
  {name: 'Collect', body: 'The agent reads what runs on its host and its configuration.'},
  {name: 'Send', body: 'It pushes a snapshot to the server, filtered of every secret.'},
  {name: 'Link', body: 'The server normalizes it and resolves the links between apps and services.'},
  {name: 'Explore', body: 'The web interface draws the graph, the history and the impact.'},
];

type Screenshot = {name: string; title: string; body: string};

// Taken by `npm run screenshots` in web/ on the fixtures of testdata/snapshots.
const hero: Screenshot = {
  name: 'graph-zone',
  title: 'The projects lens, with the shop project opened',
  body: 'Its apps, webservers and runtimes, and the services they depend on.',
};

const gallery: Screenshot[] = [
  {name: 'graph-infra', title: 'Infrastructure lens', body: 'Hosts and the links between them, similar hosts stacked.'},
  {name: 'project', title: 'A project', body: 'Every deployment of its apps, with versions and commits per env.'},
  {name: 'host', title: 'A host', body: 'The apps it runs, their framework, runtime, domains and dependencies.'},
  {name: 'namespaces', title: 'Namespaces', body: 'One per team or customer, each with its own permissions.'},
];

function Shot({shot, className}: {shot: Screenshot; className?: string}): ReactNode {
  return (
    <figure className={`${styles.shot} ${className ?? ''}`}>
      <ThemedImage
        sources={{
          light: useBaseUrl(`/img/screenshots/${shot.name}-light.webp`),
          dark: useBaseUrl(`/img/screenshots/${shot.name}-dark.webp`),
        }}
        alt={shot.title}
        width={2560}
        height={1600}
        loading="lazy"
      />
      <figcaption>
        <strong>{shot.title}</strong> {shot.body}
      </figcaption>
    </figure>
  );
}

const quickStart = `# On the server
lacis-server db migrate -c /etc/lacis
lacis-server start -c /etc/lacis
lacis-server token create   # prints an enrollment token

# On each host, with the token in /etc/lacis/agent.yaml
lacis-agent start -c /etc/lacis`;

export default function Home(): ReactNode {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout title="Infrastructure inventory" description={siteConfig.tagline}>
      <header className={styles.hero}>
        <div className="container">
          <ThemedImage
            className={styles.logo}
            sources={{light: useBaseUrl('/img/logo.svg'), dark: useBaseUrl('/img/logo-dark.svg')}}
            alt=""
            width={72}
            height={72}
          />
          <h1 className={styles.title}>{siteConfig.title}</h1>
          <p className={styles.tagline}>
            Map your servers, the apps they run and the services those apps depend on.
          </p>
          <div className={styles.actions}>
            <Link className="button button--primary button--lg" to="/docs">
              Get started
            </Link>
            <Link className="button button--secondary button--lg" to="/docs/installation">
              Install
            </Link>
          </div>
        </div>
      </header>

      <main>
        <section className={styles.section}>
          <div className="container">
            <div className={styles.features}>
              {features.map((f) => (
                <div key={f.title} className={styles.feature}>
                  <h3>{f.title}</h3>
                  <p>{f.body}</p>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className={`${styles.section} ${styles.alt}`}>
          <div className="container">
            <h2 className={styles.heading}>How it works</h2>
            <ol className={styles.steps}>
              {steps.map((s, i) => (
                <li key={s.name} className={styles.step}>
                  <span className={styles.stepNumber}>{i + 1}</span>
                  <strong>{s.name}</strong>
                  <p>{s.body}</p>
                </li>
              ))}
            </ol>
          </div>
        </section>

        <section className={styles.section}>
          <div className="container">
            <h2 className={styles.heading}>A look inside</h2>
            <Shot shot={hero} className={styles.heroShot} />
            <div className={styles.gallery}>
              {gallery.map((g) => (
                <Shot key={g.name} shot={g} />
              ))}
            </div>
          </div>
        </section>

        <section className={`${styles.section} ${styles.alt}`}>
          <div className="container">
            <h2 className={styles.heading}>Quick start</h2>
            <div className={styles.code}>
              <CodeBlock language="bash">{quickStart}</CodeBlock>
            </div>
            <p className={styles.more}>
              <Link to="/docs/installation">Installation</Link> covers the binaries, the
              Docker image and the configuration of both sides.
            </p>
          </div>
        </section>
      </main>
    </Layout>
  );
}
