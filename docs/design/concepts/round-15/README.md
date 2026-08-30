# Interface visioning — round 15

Under #634. The owner's ask (chat, 2026-08-30, verbatim):

> "I meant mock ups - are any elements now missing? We should use the
> tom (admin) bit as a menu button. Settings, license declarations etc
> live in here. As does theme switching and any other areas that need
> some way to navigate to them. Group them sensibly with a subtle
> divider. Make the menu satisfying to use, and elegant."

**Missing-elements audit** (deck + atlas vs the shipped app's surfaces):
Stream, Metrics (three views), Flags, Watchlist, Audit log, Settings,
Fleet, Entities, Run setup…, About & licence, change passphrase and
sign out all have homes in the deck or the atlas. The one true orphan
was **theme switching** — the retired Toolbar's Theme control had no
home anywhere in the mockups. The menu gives it one.

**The account menu** (`account-menu.html`, on the accepted C scene):
"tom (admin)" is a pill button; it opens a small glass drawer anchored
to the chip. Groups, hairline-divided, no labels:

1. Theme — Dark · Light · Auto as an inline segmented control, live.
2. Operate — Settings, Run setup…, Fleet, Entities, Audit log.
3. Account — Change passphrase, Sign out.
4. About & licence, with the version and AGPL-3.0 mark on the row.

Satisfying/elegant: 160 ms scale-and-fade from the chip's corner with a
gentle per-group stagger, hover lifts to full ink on a faint wash, the
chip wears a ring while open, esc or a click away closes and returns
focus, `prefers-reduced-motion` kills the motion.

Design note for the next round: the operate pages now appear in both
the atlas ports and this menu. Recommendation — the atlas slims to the
deck + zones + the reach gesture, and the menu becomes the one home for
operate/account surfaces; not applied here, awaiting the owner's view.

## Owner verdicts

- Pending: the menu (grouping, contents, feel), and the
  atlas-slimming recommendation above.
