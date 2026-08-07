<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Tells an admin that mikroview is running with a value different from
  // the one they configured.
  //
  // Deliberately dismissable only for the current page view, not
  // persisted: the condition is still true after a reload, and a
  // permanently dismissable warning is a permanently dismissed one. The
  // measured clickthrough on browser security warnings is around 70%,
  // so a banner people can make go away forever is a banner that does
  // nothing.
  import { configProblemsState } from '../lib/configProblems.svelte'

  configProblemsState.ensureLoaded()
</script>

{#if configProblemsState.hasProblems && !configProblemsState.dismissed}
  <div class="banner" role="status">
    <div class="content">
      <strong>
        {configProblemsState.problems.length === 1
          ? 'A setting in your configuration is being ignored'
          : `${configProblemsState.problems.length} settings in your configuration are being ignored`}
      </strong>
      <ul>
        {#each configProblemsState.problems as p (p.code + p.key)}
          <li>
            <code>{p.key}</code> — {p.message}
            {#if p.applied}
              <span class="applied">Using <code>{p.applied}</code> instead.</span>
            {/if}
            {#if p.remediation}
              <span class="fix">{p.remediation}</span>
            {/if}
          </li>
        {/each}
      </ul>
    </div>
    <button
      type="button"
      class="dismiss"
      onclick={() => (configProblemsState.dismissed = true)}
      aria-label="Hide until reload"
      title="Hide until reload -- the problem is still there"
    >
      ✕
    </button>
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: flex-start;
    gap: 0.75rem;
    padding: 0.6rem 0.9rem;
    /* --row-reject-bg / --reject, matching ConnectionBanner's severity
       styling. These are colourway-aware, so the banner re-tints with
       the rest of the UI; a hardcoded hex would not. */
    background: var(--row-reject-bg);
    color: var(--reject);
    border-bottom: 1px solid var(--border);
    font-size: 0.8rem;
    line-height: 1.45;
  }

  .content {
    flex: 1;
    min-width: 0;
  }

  ul {
    margin: 0.35rem 0 0;
    padding-left: 1.1rem;
  }

  li {
    margin-bottom: 0.2rem;
  }

  code {
    font-family: var(--font-mono, monospace);
    font-size: 0.95em;
  }

  .applied,
  .fix {
    color: var(--fg-muted);
    margin-left: 0.35rem;
  }

  .dismiss {
    background: none;
    border: none;
    color: var(--fg-muted);
    cursor: pointer;
    font-size: 0.9rem;
    padding: 0.1rem 0.25rem;
    flex-shrink: 0;
  }

  .dismiss:hover {
    color: var(--fg);
  }
</style>
