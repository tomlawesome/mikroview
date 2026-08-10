<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // This overlay exists to satisfy the AGPL, not as a nicety.
  //
  // Section 0 defines "Appropriate Legal Notices" as a notice displaying
  // (a) an appropriate copyright notice, (b) that there is no warranty,
  // (c) that licensees may convey the work under the License, and (d)
  // how to view a copy of the License. Section 5(d) requires an
  // interactive interface to display them, and section 13 requires that
  // anyone interacting over a network is offered the Corresponding
  // Source.
  //
  // All four notices plus the source offer live here. Removing or
  // emptying this component puts the project out of compliance -- if the
  // UI is restructured, the notices move, they don't disappear.
  import { versionState } from '../lib/version.svelte'

  let { open = $bindable(false) }: { open?: boolean } = $props()

  const SOURCE_URL = 'https://github.com/tomlawesome/mikroview'
  const LICENSE_URL = 'https://www.gnu.org/licenses/agpl-3.0.html'

  function close() {
    open = false
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) close()
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if open}
  <div class="backdrop" onclick={onBackdropClick} role="presentation">
    <div class="modal" role="dialog" aria-modal="true" aria-label="About MikroView" tabindex="-1">
      <div class="modal-header">
        <span class="title">About MikroView</span>
        <button type="button" class="close" onclick={close} aria-label="Close">✕</button>
      </div>

      <div class="body">
        {#if versionState.version}
          <p class="version">Version {versionState.version}</p>
        {/if}

        <!-- (a) copyright notice -->
        <p>Copyright © 2026 Tom Lawson</p>

        <!-- (c) licensees may convey under this License, and (d) how to view it -->
        <p>
          MikroView is free software: you can redistribute it and/or modify it
          under the terms of the
          <a href={LICENSE_URL} target="_blank" rel="noopener noreferrer">
            GNU Affero General Public License, version 3
          </a>.
        </p>

        <!-- (b) no warranty -->
        <p>
          MikroView is distributed in the hope that it will be useful, but
          <strong>without any warranty</strong> — without even the implied
          warranty of merchantability or fitness for a particular purpose. See
          the licence for details.
        </p>

        <!-- AGPL section 13: the source offer to network users -->
        <p>
          The complete source code for this version is available at
          <a href={SOURCE_URL} target="_blank" rel="noopener noreferrer">
            {SOURCE_URL}
          </a>.
        </p>

        <p class="commercial">
          A commercial licence is available if you want to use MikroView in a
          way the AGPL doesn't permit — see
          <a href="{SOURCE_URL}/blob/main/COMMERCIAL-LICENSE.md" target="_blank" rel="noopener noreferrer">
            COMMERCIAL-LICENSE.md
          </a>.
        </p>

        <!--
          Third-party attribution, distinct from the AGPL notices above:
          those are MikroView's licence to you, this is the copyright and
          licence text of the software MikroView itself distributes.
          MIT/BSD/ISC/Apache-2.0 all require those to accompany a binary
          distribution, and the runtime image is distroless — this binary
          is the whole artefact — so the notices are embedded in it and
          served from here rather than left in a file nobody receives.
        -->
        <p class="third-party">
          MikroView includes third-party open-source software. Their copyright
          notices and licences are at
          <a href="/api/third-party-notices" target="_blank" rel="noopener noreferrer">
            third-party notices
          </a>.
        </p>
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }

  .modal {
    background: var(--bg-elevated, var(--bg));
    border: 1px solid var(--border);
    border-radius: 8px;
    max-width: 32rem;
    width: calc(100% - 2rem);
    max-height: calc(100vh - 4rem);
    overflow-y: auto;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border);
  }

  .title {
    font-weight: 600;
  }

  .close {
    background: none;
    border: none;
    color: var(--fg-muted);
    cursor: pointer;
    font-size: 1rem;
    padding: 0.25rem;
  }

  .close:hover {
    color: var(--fg);
  }

  .body {
    padding: 1rem;
    font-size: 0.85rem;
    line-height: 1.5;
  }

  .body p {
    margin: 0 0 0.75rem;
  }

  .body p:last-child {
    margin-bottom: 0;
  }

  .version {
    color: var(--fg-muted);
    font-family: var(--font-mono, monospace);
    font-size: 0.8rem;
  }

  .commercial {
    color: var(--fg-muted);
    border-top: 1px solid var(--border);
    padding-top: 0.75rem;
  }

  .third-party {
    color: var(--fg-muted);
  }

  .body a {
    color: var(--accent);
    word-break: break-word;
  }
</style>
