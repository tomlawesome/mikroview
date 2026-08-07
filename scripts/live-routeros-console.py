#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
#
# Drives a booted RouterOS CHR over its serial console. Companion to
# live-routeros.sh, which owns the VM's lifecycle; this file only knows
# how to talk to one that is already up.
#
# Usage:
#   live-routeros-console.py '/system resource print' ':put [/user find]'
#   live-routeros-console.py --port 15901 --timeout 120 '<command>'
#
# Each argument is one console command; output is printed under a header
# per command. A non-zero exit means a command never came back.

import argparse
import re
import socket
import sys
import time

# +c disables console colour and +t suppresses terminal capability
# detection. Without +t RouterOS sends ESC-Z and waits for a reply that a
# raw socket never sends, which stalls the login for tens of seconds and
# looks exactly like a hung boot. 200w widens output so RouterOS stops
# wrapping and truncating its own tables at 80 columns.
LOGIN_FLAGS = '+ct200w'

PROMPT = re.compile(r'\[[^\]@]+@[^\]]*\] ?> ?$')
LOGIN_PROMPT = re.compile(r'Login: ?$')
PASSWORD_PROMPT = re.compile(r'[Pp]assword: ?$')
LICENCE_PROMPT = re.compile(r'\[Y/n\]: ?$')
# RouterOS 7 offers a password change on first login and will not reach a
# prompt until it is answered. The fixture skips it: the VM's only
# exposure is a loopback serial port -- QEMU user-mode networking forwards
# nothing inbound -- so a blank admin password is unreachable, and a
# router that keeps its factory credentials is closer to what an operator
# following the docs will be sitting in front of anyway.
PASSWORD_CHANGE = re.compile(r'new password> ?$')
# RouterOS pauses long output. 'D' dumps the remainder; 'Q' would quit and
# lose it, which is the tempting and wrong answer.
PAGING = re.compile(r'-- \[Q quit\|D dump\|[^\]]*\]')
ANSI = re.compile(r'\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[cZ=>]|\r')
# A read can land mid-escape-sequence. Anything matching this at the end
# of the buffer is held back until the rest of it arrives, rather than
# being printed as stray '[9999B' litter through the transcript.
PARTIAL_ANSI = re.compile(r'\x1b(\[[0-9;?]*|\][^\x07]*)?$')


class Console:
    def __init__(self, host='127.0.0.1', port=15901, timeout=120,
                 user='admin', password=''):
        self.s = socket.create_connection((host, port), timeout=15)
        self.s.settimeout(0.5)
        self.timeout = timeout
        self.user = user
        self.password = password
        self.raw = ''
        self.buf = ''

    def _pump(self):
        try:
            data = self.s.recv(8192)
        except socket.timeout:
            return False
        if not data:
            raise EOFError('serial console closed -- is the VM still running?')
        self.raw += data.decode('utf-8', 'replace')
        held = PARTIAL_ANSI.search(self.raw)
        ready, self.raw = (self.raw[:held.start()], self.raw[held.start():]) if held else (self.raw, '')
        self.buf += ANSI.sub('', ready)
        return True

    def read_until(self, rx, timeout=None):
        deadline = time.time() + (timeout or self.timeout)
        while time.time() < deadline:
            match = rx.search(self.buf)
            if match:
                out, self.buf = self.buf[:match.end()], self.buf[match.end():]
                return out
            if PAGING.search(self.buf):
                self.buf = PAGING.sub('', self.buf)
                self.s.sendall(b'D')
            self._pump()
        raise TimeoutError(f'timed out waiting for {rx.pattern!r}; last saw {self.buf[-300:]!r}')

    def drain(self, quiet=0.8):
        last = time.time()
        while time.time() - last < quiet:
            if self._pump():
                last = time.time()
        out, self.buf = self.buf, ''
        return out

    def send(self, line=''):
        self.s.sendall((line + '\r').encode())

    def login(self):
        """Reach a command prompt from wherever the console currently is.

        The serial session survives a reconnect, so this lands at a login
        banner on a cold VM and at a live prompt on a warm one. A warm
        prompt belonging to a different user is logged out first, since
        the caller asked for this one.
        """
        self.send()
        seen = self.read_until(re.compile(LOGIN_PROMPT.pattern + '|' + PROMPT.pattern), 180)
        if not LOGIN_PROMPT.search(seen):
            if f'[{self.user}@' in seen:
                self.drain()
                return
            self.send('/quit')
            self.read_until(LOGIN_PROMPT, 60)
        self.send(self.user + LOGIN_FLAGS)
        self.read_until(PASSWORD_PROMPT, 60)
        self.send(self.password)  # CHR ships with a blank admin password
        deadline = time.time() + 180
        first_boot = re.compile('|'.join(
            (LICENCE_PROMPT.pattern, PASSWORD_CHANGE.pattern, PROMPT.pattern)))
        while time.time() < deadline:
            seen = self.read_until(first_boot, 180)
            if LICENCE_PROMPT.search(seen):
                self.send('n')
            elif PASSWORD_CHANGE.search(seen):
                self.s.sendall(b'\x03')  # Ctrl-C, the offered way to skip
            else:
                self.drain()
                return
        raise TimeoutError('never reached a RouterOS prompt')

    def cmd(self, command, timeout=None):
        """Run one command and return everything the console printed.

        The drain first is load-bearing: RouterOS echoes and repaints its
        own prompt, so a stale prompt left in the buffer is indis-
        tinguishable from the command having already finished.
        """
        self.drain(0.6)
        self.send(command)
        time.sleep(0.3)
        return self.read_until(PROMPT, timeout).rstrip()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--host', default='127.0.0.1')
    ap.add_argument('--port', type=int, default=15901)
    ap.add_argument('--timeout', type=int, default=120)
    ap.add_argument('--login', default='admin')
    ap.add_argument('--password', default='')
    ap.add_argument('commands', nargs='*')
    args = ap.parse_args()

    console = Console(args.host, args.port, args.timeout, args.login, args.password)
    console.login()
    for command in args.commands:
        print(f'== {command}')
        print(console.cmd(command))
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (TimeoutError, EOFError) as err:
        print(f'live-routeros-console: {err}', file=sys.stderr)
        sys.exit(1)
