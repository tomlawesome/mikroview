# Setting up an authenticator app

A second step at sign-in: after your password, MikroView asks for a
code from an app on your phone (Google Authenticator, 1Password,
Bitwarden, or similar) before it lets you in. Someone who only has your
password can't sign in without it too.

**Single sign-on accounts don't get this option.** If you sign in
through SSO, your identity provider is what verifies you, and the
account menu says so instead of offering to set one up — turning on a
second MikroView-side step would be securing a login your identity
provider already owns.

## Turning it on

Open the account menu (click your username, bottom of the rail) and
choose **Authenticator app**.

> Screenshot: the account menu open, with "Authenticator app" showing
> among the other rows (no "· on" tag yet, since nothing is set up).

Press **Set up authenticator app**. MikroView shows a QR code and, next
to it, the same secret written out as text.

> Screenshot: the enrolment screen — the QR code on the left, the text
> secret on the right, and the code box beneath them.

Scan the QR code with your app, or, if scanning doesn't work, type the
secret in by hand — it's shown for exactly that reason, not only as a
fallback. Either way your app starts showing a fresh 6-digit code every
30 seconds.

## Confirming it

Type the current code from your app into the box and press **Confirm**.
This is what actually turns the factor on — until you confirm, nothing
you scanned or typed has changed how you sign in.

Confirming also signs out every other browser or device this account is
currently signed into. You stay signed in here. If that's unexpected —
somewhere else you didn't recognise was signed in — that's worth
noticing.

## Save your recovery codes

Right after confirming, MikroView shows ten recovery codes.

> Screenshot: the recovery-codes screen — ten codes in two columns, the
> **Copy all** button, and **I have saved these**.

**This is the only time they're shown.** Each works once, in place of a
code from your app, if your phone is lost, out of battery, or just not
to hand. Save all ten somewhere safe now — a password manager is the
obvious place — before you press **I have saved these**. MikroView
can't show them to you again; if you lose them without saving, the only
way back to a fresh set is turning the factor off and setting it up
again (see "If your phone is lost for good" below).

The screen won't close by clicking outside it or pressing Escape,
unlike every other dialog in MikroView — the ten codes exist in the
clear nowhere else, so leaving has to be the deliberate "I have saved
these", not an accidental dismiss.

## Signing in afterwards

Sign in with your username and password as usual. Once your password
checks out, MikroView asks for a second code instead of taking you
straight in.

> Screenshot: the "Enter your code" screen at login, with the code box
> and the "Use a recovery code instead" link beneath it.

Type the current code from your app and continue. Get it wrong (or let
it expire) too many times and you're rate-limited the same way a wrong
password is — there's no separate, more generous allowance for guessing
the second step.

## Using a recovery code

If your phone isn't available — dead battery, no signal on the app
itself, left at home — press **Use a recovery code instead** on the code
screen and type one of the ten you saved instead of a 6-digit code.
It works exactly once: MikroView marks it spent the moment it's
accepted, so save the ones you have left, and once you're back on your
phone, consider turning the factor off and back on to draw a fresh set
(the only way to get one — there's no separate "top up my codes"
action).

## If your phone is lost for good

**You still have a password and at least one recovery code.** Sign in
with a recovery code as above, then open the account menu →
**Authenticator app** → **Turn off** (this needs your password, the same
guard as changing it). Set it up again from your new phone whenever
you're ready — enrolling fresh replaces the old secret and issues a new
set of ten codes.

**You have a password but no recovery codes left.** Ask your admin to
clear the factor from your account: Settings → **people** → your row →
**clear authenticator app**. This needs your admin to be signed in and
does not need your password — they're standing in for a credential you
no longer have a way to prove, the same reasoning behind an admin
resetting someone's password. Once it's cleared, sign in with your
password alone and set the factor up again if you want one.

**You're the admin, and it's your own factor that's stuck.** The Settings
button above deliberately refuses to clear the admin's own factor — an
admin who could clear their own second step from a signed-in session
could just as easily clear anyone's. Instead, from the machine or
container MikroView runs on:

```sh
mikroview -clear-second-factor <your-username>
```

This is the same shape as `mikroview -recover-admin-account`: it needs
host access and one of your recovery keys, and using it rotates your
recovery keys, so save the new set it prints before confirming. See
[docs/configuration.md](configuration.md#recovery-keys) if you're not
sure what a recovery key is or how to generate one.

**You've also forgotten your password.** That's a different problem —
see [docs/configuration.md](configuration.md#resetting-someones-password)
(an admin can help) or
[docs/configuration.md](configuration.md#recovering-the-admin-account)
if it's the admin account itself.
