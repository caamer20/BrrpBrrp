# Your account and security

**For members.** Your account is a username, a password, and optionally a second factor. This
chapter covers keeping them safe and getting back in if something goes wrong.

## Your password

Passwords are 12 to 72 bytes. Choose a long phrase you do not use anywhere else. Change it in
**Your preferences → Account**: enter your current password, then the new one.

Changing your password signs out **all** your sessions, so you sign in again on every device. A
password change does not turn off two-factor sign-in.

## Two-factor sign-in

Two-factor sign-in asks for a six-digit code from an authenticator app as well as your password.
It is strongly recommended for everyone, and it protects you if your password is ever guessed or
leaked.

### Turn it on

1. Open **Your preferences → Account** and find **Two-factor authentication**.
2. Add the displayed setup key to an authenticator app (any standard TOTP app will do).
3. Enter the six-digit code the app shows, **within ten minutes**. Codes depend on the time, so keep
   your phone's clock accurate.
4. **Save these recovery codes now.** You are shown ten single-use codes once. Select **I have saved my
   codes** only after you have stored them somewhere private.

Turning two-factor sign-in on or off signs out your other sessions.

### Sign in with it

Sign in with your password as usual, then enter a current code from your app, or one of your
recovery codes. A recovery code works once. The **Authenticator or recovery code** field accepts
either.

### Lost your phone or your codes

Use one of these routes, the first that applies:

1. **A recovery code.** Sign in with one of the codes you saved. Then enrol a new authenticator
   straight away and save a new set of recovery codes.
2. **An administrator removes the lost factor.** If the codes are gone too, ask an administrator
   to remove your second factor from their People list. This ends your sessions, so you sign in
   with your password alone and must enrol a new factor immediately.
3. **The owner's host operator.** The owner's own second factor can only be recovered on the
   server host, by the operator. That path is described in [Administrators](administrators.md#recovering-access-when-someone-is-locked-out).

Nobody can restore your access from a browser or by e-mail. The server has no e-mail address
for accounts and no account-recovery service. Keeping your recovery codes safe is the only
protection against being locked out.

## Signed-in devices

**Your preferences → Account** lists the devices where you are signed in, by browser and
platform. The current device is marked **This device**. To end every other session at once, choose
**Sign out other devices**. Use it after lending a computer or if you see a device you do not recognise.

Some actions ask you to **confirm your sign-in** again first, with your current password or a
current code. This includes changing a second factor and downloading your data export.

## Your data export

Next to your name at the foot of the sidebar, **Your data export** downloads an archive of your own
data: the account details the server holds for you, your own messages, and the conversations you can
read. It needs a recent sign-in. The export includes only conversations you can read, so a private conversation
you are not part of is not included.

Keep the archive private. It contains your messages and personal details, and it is not encrypted.

## Leaving the server

A member cannot remove their own account from the app. Removing an account is an administrator's
action, so if you want to leave, ask an administrator. Before you do, you can take your own data
with **Your data export**. Whether a future release adds a self-service way to leave is an open
question for the project's owner; the [Known limitations](../known-limitations.md#leaving-a-community)
records the current position.

## Good habits

- Use a different password for this server than for anything else.
- Turn on two-factor sign-in and store the recovery codes away from your computer.
- Sign out on shared computers, and sign out other devices when you lend yours.
- Check the server's address before you sign in. A look-alike address with a sign-in page is the most
  common way accounts are taken.
- Report suspicious messages with **Report message**. See [Privacy and safety](privacy-and-safety.md).
