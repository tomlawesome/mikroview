<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The body of #445's NAT lookup, rendered identically by the two
  // surfaces that host it: the anchored popover on desktop
  // (RouterLookupPopover.svelte) and a section of the detail sheet on
  // mobile (EventDetailSheet.svelte). Extracted rather than written
  // twice, because the wording *is* the feature -- two copies would
  // eventually say two different things about how much the product
  // knows, which is the exact failure this popup exists to prevent.
  //
  // The two modes and why they must never share a rendering are
  // documented in lib/routerLookup.svelte.ts. Here they are simply two
  // separate branches with nothing in common but the entry shape:
  //
  //   logged      -- the operator's log-prefix names the rule. A fact.
  //   not logged  -- nothing names a rule, so the table is split into
  //                  what this event could have come from and what it
  //                  rules out, with the reason against each exclusion.
  //   the floor   -- a push carrying nothing to subtract on: the whole
  //                  table, and a plain statement that narrowing needs
  //                  an updated push script.
  //
  // Everything shown here is pushed router state (issue #186). Nothing
  // in this path contacts the router, and nothing infers a rule the
  // router did not report.
  import { routerLookupState as st } from '../lib/routerLookup.svelte'

  const partition = $derived(st.natPartition)
</script>

{#if st.loading}
  <div class="status">Loading…</div>
{:else if st.error}
  <div class="status error">{st.error}</div>
{:else if !st.available}
  <div class="status">
    No NAT table pushed by “{st.device}” yet — this data arrives via the RouterOS push
    integration, not syslog.
  </div>

  <!-- ---- Layer 1: the operator logged the rule, so it is named ---- -->
{:else if st.natMode === 'logged'}
  {#if st.natMatches.length === 0}
    <div class="status">
      No rule in the pushed NAT table ({st.tableSize} rules) carries the log-prefix “{st.ruleLabel}”.
      The event was logged with it, so either the table was pushed before the rule was tagged, or
      the prefix has changed since.
    </div>
  {:else}
    {#if st.natMatches.length > 1}
      <!-- The same multi-match honesty filter rules get: a shared prefix
           resolves to every rule carrying it, never to an arbitrary pick
           from among them. -->
      <div class="state-line">
        {st.natMatches.length} rules share the prefix “{st.ruleLabel}” — one of them performed this
        translation.
      </div>
    {/if}
    <div class="entries">
      {#each st.natMatches as r (r.ordinal)}
        <div class="entry">
          <div class="entry-header">
            <span class="ordinal">#{r.ordinal}</span>
            <span class="chain">{r.chain}</span>
            <span class="badge">{r.action}</span>
          </div>
          {#if r.comment}
            <div class="comment">{r.comment}</div>
          {:else}
            <div class="comment dim">no comment set on this rule</div>
          {/if}
        </div>
      {/each}
    </div>
    <div class="footnote">
      Numbered as RouterOS numbers them — “go look at rule {st.natMatches[0].ordinal} in RouterOS”.
    </div>
  {/if}
{:else if st.natRules.length === 0}
  <!-- The rule mode has had an empty state since it was written; this
       one did not (#267, Uncertain), so a router that pushed a NAT table
       with no rules in it -- entirely ordinary, plenty of routers do no
       NAT -- got an empty box and a footnote explaining how to read
       rules that are not there. -->
  <div class="status">
    “{st.device}” has pushed its NAT table and it is empty — no NAT rules are configured on that
    router.
  </div>

  <!-- ---- Layer 3: the floor, a push with nothing to subtract on ---- -->
{:else if partition && !partition.discriminable}
  <div class="entries">
    {#each st.natRules as r (r.ordinal)}
      <div class="entry">
        <div class="entry-header">
          <span class="ordinal">#{r.ordinal}</span>
          <span class="chain">{r.chain}</span>
          <span class="badge">{r.action}</span>
        </div>
        {#if r.comment}
          <div class="comment">{r.comment}</div>
        {/if}
      </div>
    {/each}
  </div>
  <div class="footnote">
    The full pushed NAT table — a log line shows the translation result, never which rule performed
    it, so match it up by eye.
    <p class="floor">
      This push predates the fields needed to rule anything out — showing the whole table. Update
      the push script to enable narrowing.
    </p>
  </div>

  <!-- ---- Layer 2: not logged, so subtraction, shown working ---- -->
{:else if partition}
  <div class="state-line">
    This translation was not logged, so no rule can be named. Rules are shown by what this event
    can rule out.
  </div>
  <div class="evidence">
    {#if st.natEvidence === 'group-head'}
      Evaluated against the first event in this group — members can differ; open the group to check
      one exactly.
    {:else}
      Evaluated against this row’s event.
    {/if}
  </div>

  <h3 class="section">
    Could have performed it — {partition.couldHave.length} of {partition.total}
  </h3>
  {#if partition.couldHave.length === 0}
    <div class="status">
      Nothing in the pushed table could have performed this translation — the table is probably
      older than the rule that did.
    </div>
  {:else}
    <div class="entries">
      {#each partition.couldHave as v (v.rule.ordinal)}
        <div class="entry">
          <div class="entry-header">
            <span class="ordinal">#{v.rule.ordinal}</span>
            <span class="chain">{v.rule.chain}</span>
            <span class="badge">{v.rule.action}</span>
          </div>
          {#if v.rule.comment}
            <div class="comment">{v.rule.comment}</div>
          {/if}
          {#each v.notEvaluable as cond (cond)}
            <div class="detail">{cond} — not evaluable here</div>
          {/each}
        </div>
      {/each}
    </div>
  {/if}

  <h3 class="section">Ruled out by this event — {partition.ruledOut.length}</h3>
  {#if partition.ruledOut.length === 0}
    <div class="status">Nothing in the pushed table is contradicted by this event.</div>
  {:else}
    <div class="entries">
      {#each partition.ruledOut as v (v.rule.ordinal)}
        <div class="entry out">
          <div class="entry-header">
            <span class="ordinal">#{v.rule.ordinal}</span>
            <span class="chain">{v.rule.chain}</span>
            <span class="badge">{v.rule.action}</span>
          </div>
          {#if v.rule.comment}
            <div class="comment">{v.rule.comment}</div>
          {/if}
          <div class="detail reason">ruled out: {v.ruledOut}</div>
        </div>
      {/each}
    </div>
  {/if}

  <div class="footnote">
    “Could have” is not “did”. For an exact answer, log the NAT rules you care about: set
    <code>log=yes log-prefix=…</code> on the rule in RouterOS — logged translations resolve to their
    rule by name. MikroView never touches the router; that is a command for you to run.
  </div>
{/if}

<style>
  .status {
    color: var(--fg-dim);
    padding: 4px 0;
  }

  .status.error {
    color: var(--reject);
  }

  /* The first thing in the body of the mode that needs it: what this
     lookup is and is not claiming, before any rule is shown. */
  .state-line {
    color: var(--fg);
    margin-bottom: 6px;
  }

  .evidence {
    color: var(--fg-dim);
    font-size: 11.5px;
    margin-bottom: 10px;
  }

  /* Real headings, so a screen reader narrates the partition rather than
     presenting one undifferentiated list of rules. */
  .section {
    font-size: 12px;
    font-weight: 600;
    color: var(--fg);
    margin: 12px 0 6px;
  }

  .entries {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .entry + .entry {
    border-top: 1px solid var(--border);
    padding-top: 8px;
  }

  .entry-header {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }

  .ordinal {
    font-family: var(--font-mono);
    font-weight: 700;
    color: var(--fg);
  }

  .chain {
    font-family: var(--font-mono);
    color: var(--fg-muted);
    flex: 1;
  }

  /* De-emphasis, not disablement: a ruled-out rule is still content the
     operator reads, because reading it is how they audit the partition
     instead of trusting it. Colour steps down one level; nothing here
     drops it out of the accessibility tree or below readable contrast,
     and the reason itself is text. */
  .entry.out .ordinal {
    color: var(--fg-muted);
    font-weight: 600;
  }

  .entry.out .comment {
    color: var(--fg-dim);
  }

  .badge {
    flex: none;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.02em;
    padding: 1px 6px;
    border-radius: 4px;
    color: var(--fg-muted);
    border: 1px solid var(--border);
  }

  .comment {
    color: var(--fg);
    margin-top: 3px;
    overflow-wrap: anywhere;
  }

  .comment.dim {
    color: var(--fg-dim);
    font-style: italic;
  }

  .detail {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 2px;
    overflow-wrap: anywhere;
  }

  .detail.reason {
    color: var(--fg-muted);
  }

  .footnote {
    margin-top: 10px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
    color: var(--fg-dim);
    font-size: 11.5px;
  }

  .footnote code {
    font-family: var(--font-mono);
    font-size: 11px;
  }

  .footnote .floor {
    margin: 6px 0 0;
  }
</style>
