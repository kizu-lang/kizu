// Records the TLS fixtures under examples/fixtures/tls/: one connection
// between a Kizu program and node's TLS, each record either side sent, in the
// order they reached the relay between them. tests/behavior/src/tls_replay/
// replays them with node out of the loop: the Kizu end must send the same
// bytes, and must accept the bytes node sent.
//
// Run it the one way, from the repository root:
//
//     nix develop -c just tls-fixtures
//
// which is `node scripts/record-tls-fixtures/record.mjs <kizu>` with the
// compiler the pinned seed built (target/seed/stage1/bin/kizu, `just stage1`).
// It takes no arguments and records every case below each time, so that every
// fixture comes from the same node. It refuses a node that is not the
// devShell's (one under /nix/store/), and it writes nothing unless every case
// went the way the replay expects -- node's side of it included.
//
// Each case runs its Kizu program with `<kizu> run scripts/record-tls-fixtures/
// <program>.kizu -- <args>` from the repository root, and node takes the other
// end:
//
//     Kizu program  --TCP--  relay (this script)  --TCP--  node tls / https
//
// client-echo-p256 / -p384 / -rsa  client.kizu against a tls server that
//                                   answers in upper case
// client-wrong-host                 client.kizu asking for example.test
// client-wrong-root                 client.kizu trusting other-p256
// server-echo-p256 / -p384 / -rsa   server.kizu against a tls client that says
//                                   "hello over tls"
// server-wrong-root                 server.kizu against a client trusting
//                                   other-p256
// https-client-fetch                https_client.kizu against an https server
//                                   that answers with the path
// https-client-wrong-root           https_client.kizu trusting other-p256
// https-client-wrong-host           https_client.kizu asking for 127.0.0.1
// https-server-accept               https_server.kizu answering with `accept`:
//                                   https-server-accept-one / -two /
//                                   -stranger / -three, one connection each
// https-server-loop                 https_server.kizu answering with `first` /
//                                   `next`: https-server-keepalive (/one and
//                                   /two), -stranger, -fresh (/three)
//
// The Kizu end draws from the predictable source seeded with
// SHA-256("kizu tls fixture " + case), so recording again changes only the
// node lines and whatever the change being recorded changed. An https server
// case is one run of the server and writes a fixture per connection, all
// with the case's seed: the server gives each connection it accepts the next
// split of it, so the fixtures hold in the order the run took them.
//
// A fixture is text, one record a line after a header:
//
//     # kizu tls trace 1
//     # case: client-echo-p256
//     # peer: node v20.18.1 OpenSSL 3.0.14
//     # peer-role: tls server, answers in upper case
//     # recorder: scripts/record-tls-fixtures/record.mjs (regenerate: nix develop -c just tls-fixtures)
//     # kizu: client host=localhost roots=localhost-p256 now=1893456000
//     # seed: <SHA-256 of "kizu tls fixture client-echo-p256">
//     # suite: TLS_AES_128_GCM_SHA256
//     # keylog: <label> <secret>          (node's keylog, one line a secret)
//     # outcome: <what node saw>
//     # port: <the port Kizu dialled>     (https-client-fetch only)
//     # sha256: <of the file without this line>
//     > 16030100b8...  ; ClientHello 180
//     < 160303007a...  ; ServerHello 118
//
// `>` is a record the Kizu end sent and `<` one node sent, header included.
// What follows `;` is a note the replay does not read: what the record holds,
// opened here with node's secrets and node's own HKDF and AEAD.

import { spawn } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import https from 'node:https';
import net from 'node:net';
import path from 'node:path';
import tls from 'node:tls';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const fixtures = path.join(root, 'examples/fixtures/tls');
const recorder = 'scripts/record-tls-fixtures/record.mjs';
const now = 1893456000;

function fail(message) {
  throw new Error(message);
}

function pem(name) {
  return fs.readFileSync(path.join(fixtures, name));
}

// TLS 1.3 and X25519 only, and no ALPN. The suites are the two Kizu offers,
// AES-128-GCM first and in that order: node's default puts ChaCha20-Poly1305
// ahead of it and chooses by its own order, and the cases expect the suite
// they have always seen. (node takes the TLS 1.3 suites in `ciphers`.)
const tlsOptions = {
  minVersion: 'TLSv1.3',
  maxVersion: 'TLSv1.3',
  ecdhCurve: 'X25519',
  ciphers: 'TLS_AES_128_GCM_SHA256:TLS_CHACHA20_POLY1305_SHA256',
  honorCipherOrder: true,
};

// --- the relay -------------------------------------------------------------

// relay listens on a free port and passes every connection it takes on to
// `target()`'s port, keeping the records each way. `kizuDialled` says which
// end the Kizu program is: the one that dialled the relay (a Kizu client) or
// the one the relay dials (a Kizu server). Each connection's trace resolves
// once both ends are closed.
function relay(target, kizuDialled) {
  const traces = [];
  const waiting = [];
  const server = net.createServer((down) => {
    const up = net.connect(target(), '127.0.0.1');
    const records = [];
    const kept = { down: Buffer.alloc(0), up: Buffer.alloc(0) };
    const mark = { down: kizuDialled ? '>' : '<', up: kizuDialled ? '<' : '>' };
    let closed = 0;
    let broken = null;
    const take = (side, chunk) => {
      let held = Buffer.concat([kept[side], chunk]);
      while (held.length >= 5) {
        const length = held.readUInt16BE(3);
        if (held.length < 5 + length) {
          break;
        }
        records.push({ mark: mark[side], bytes: Buffer.from(held.subarray(0, 5 + length)) });
        held = held.subarray(5 + length);
      }
      kept[side] = held;
    };
    const finish = () => {
      closed += 1;
      if (closed < 2) {
        return;
      }
      if (kept.down.length > 0 || kept.up.length > 0) {
        broken = broken ?? 'a record was cut short by the close';
      }
      const trace = { records, broken };
      const next = waiting.shift();
      if (next) {
        next(trace);
      } else {
        traces.push(trace);
      }
    };
    // Bytes go on as they come; a record is kept when its last byte arrives,
    // so a Kizu record can only be kept after the node record it answers.
    down.on('data', (chunk) => {
      take('down', chunk);
      if (!up.destroyed) {
        up.write(chunk);
      }
    });
    up.on('data', (chunk) => {
      take('up', chunk);
      if (!down.destroyed) {
        down.write(chunk);
      }
    });
    down.on('end', () => up.end());
    up.on('end', () => down.end());
    // A reset after the last record is how some closes end; the trace notes
    // nothing for it, and the replay does not depend on it.
    down.on('error', () => {});
    up.on('error', () => {});
    down.on('close', finish);
    up.on('close', finish);
  });
  const listening = new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve(server.address().port)));
  return {
    listening,
    next: () => (traces.length > 0 ? Promise.resolve(traces.shift()) : new Promise((resolve) => waiting.push(resolve))),
    close: () => server.close(),
  };
}

// --- the Kizu end -----------------------------------------------------------

// kizuRun runs one of the Kizu programs next to this script and resolves its
// stdout once it exits. `onLine` sees each line as it arrives.
function kizuRun(kizu, program, args, onLine) {
  const child = spawn(kizu, ['run', `scripts/record-tls-fixtures/${program}`, '--', ...args], {
    cwd: root,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let out = '';
  let err = '';
  let partial = '';
  child.stdout.on('data', (chunk) => {
    out += chunk;
    partial += chunk;
    let at;
    while ((at = partial.indexOf('\n')) >= 0) {
      const line = partial.slice(0, at);
      partial = partial.slice(at + 1);
      if (onLine) {
        onLine(line);
      }
    }
  });
  child.stderr.on('data', (chunk) => {
    err += chunk;
  });
  const timer = setTimeout(() => child.kill(), 120000);
  const done = new Promise((resolve) => {
    child.on('close', (code) => {
      clearTimeout(timer);
      resolve({ code, out: out.trimEnd(), err });
    });
  });
  return { child, done };
}

function seedOf(name) {
  return crypto.createHash('sha256').update(`kizu tls fixture ${name}`).digest('hex');
}

// --- reading the records ----------------------------------------------------

const handshakeNames = {
  1: 'ClientHello',
  2: 'ServerHello',
  4: 'NewSessionTicket',
  5: 'EndOfEarlyData',
  8: 'EncryptedExtensions',
  11: 'Certificate',
  13: 'CertificateRequest',
  15: 'CertificateVerify',
  20: 'Finished',
  24: 'KeyUpdate',
};

const alertNames = {
  0: 'close_notify',
  10: 'unexpected_message',
  20: 'bad_record_mac',
  40: 'handshake_failure',
  42: 'bad_certificate',
  43: 'unsupported_certificate',
  44: 'certificate_revoked',
  45: 'certificate_expired',
  46: 'certificate_unknown',
  47: 'illegal_parameter',
  48: 'unknown_ca',
  50: 'decode_error',
  51: 'decrypt_error',
  70: 'protocol_version',
  80: 'internal_error',
  86: 'inappropriate_fallback',
  90: 'user_canceled',
  109: 'missing_extension',
  110: 'unsupported_extension',
  112: 'unrecognized_name',
  116: 'certificate_required',
};

const suites = {
  0x1301: { name: 'TLS_AES_128_GCM_SHA256', cipher: 'aes-128-gcm', keyLength: 16 },
  0x1303: { name: 'TLS_CHACHA20_POLY1305_SHA256', cipher: 'chacha20-poly1305', keyLength: 32 },
};

// suiteOf reads the cipher suite out of the ServerHello in `records`.
function suiteOf(records) {
  for (const record of records) {
    const bytes = record.bytes;
    if (bytes[0] === 22 && bytes[5] === 2) {
      // header 5, type 1, length 3, version 2, random 32, session id
      const idLength = bytes[5 + 4 + 2 + 32];
      const at = 5 + 4 + 2 + 32 + 1 + idLength;
      const suite = suites[bytes.readUInt16BE(at)];
      if (!suite) {
        fail(`the server chose suite ${bytes.readUInt16BE(at).toString(16)}`);
      }
      return suite;
    }
  }
  fail('no ServerHello');
}

// expandLabel is HKDF-Expand-Label (RFC 8446 §7.1) with an empty context,
// for outputs of at most one SHA-256 block.
function expandLabel(secret, label, length) {
  const full = Buffer.from(`tls13 ${label}`);
  const info = Buffer.concat([Buffer.from([length >> 8, length & 255, full.length]), full, Buffer.from([0, 1])]);
  return crypto.createHmac('sha256', secret).update(info).digest().subarray(0, length);
}

// A Direction opens what one end sent: in the clear until its handshake
// secret applies, under that until it sent Finished, under its traffic
// secret after.
class Direction {
  constructor(suite, handshakeSecret, trafficSecret) {
    this.suite = suite;
    this.secrets = [handshakeSecret, trafficSecret];
    this.epoch = 0;
    this.sequence = 0;
    this.handshake = Buffer.alloc(0);
  }

  open(record) {
    const secret = this.secrets[this.epoch];
    if (!secret) {
      fail('an encrypted record with no secret to open it');
    }
    const key = expandLabel(secret, 'key', this.suite.keyLength);
    const iv = expandLabel(secret, 'iv', 12);
    const nonce = Buffer.from(iv);
    for (let index = 0; index < 8; index += 1) {
      nonce[11 - index] ^= Number((BigInt(this.sequence) >> BigInt(8 * index)) & 255n);
    }
    this.sequence += 1;
    const body = record.subarray(5);
    const decipher = crypto.createDecipheriv(this.suite.cipher, key, nonce, { authTagLength: 16 });
    decipher.setAAD(record.subarray(0, 5));
    decipher.setAuthTag(body.subarray(body.length - 16));
    const inner = Buffer.concat([decipher.update(body.subarray(0, body.length - 16)), decipher.final()]);
    let end = inner.length;
    while (end > 0 && inner[end - 1] === 0) {
      end -= 1;
    }
    if (end === 0) {
      fail('an inner plaintext with no content type');
    }
    return { type: inner[end - 1], content: inner.subarray(0, end - 1) };
  }

  // messages names the handshake messages `content` completes.
  messages(content) {
    this.handshake = Buffer.concat([this.handshake, content]);
    const names = [];
    while (this.handshake.length >= 4) {
      const length = this.handshake.readUIntBE(1, 3);
      if (this.handshake.length < 4 + length) {
        break;
      }
      const type = this.handshake[0];
      names.push(`${handshakeNames[type] ?? `handshake ${type}`} ${length}`);
      this.handshake = this.handshake.subarray(4 + length);
      if (type === 20) {
        this.epoch = 1;
        this.sequence = 0;
      }
    }
    if (this.handshake.length > 0) {
      names.push('(continues)');
    }
    return names.join(', ');
  }

  note(record) {
    const type = record[0];
    if (type === 20) {
      return 'change_cipher_spec';
    }
    if (type === 21) {
      return `alert ${alertNames[record[6]] ?? record[6]} (in the clear)`;
    }
    if (type === 22) {
      return this.messages(record.subarray(5));
    }
    if (type !== 23) {
      fail(`a record of type ${type}`);
    }
    const inner = this.open(record);
    if (inner.type === 22) {
      return this.messages(inner.content);
    }
    if (inner.type === 21) {
      return `alert ${alertNames[inner.content[1]] ?? inner.content[1]}`;
    }
    if (inner.type === 23) {
      return `application_data ${inner.content.length}`;
    }
    fail(`an inner content type ${inner.type}`);
  }
}

// notes opens every record with node's secrets and says what each holds.
// `kizuIsClient` says which secrets the `>` records were sealed under.
function notes(records, suite, keylog, kizuIsClient) {
  const client = new Direction(suite, keylog.CLIENT_HANDSHAKE_TRAFFIC_SECRET, keylog.CLIENT_TRAFFIC_SECRET_0);
  const server = new Direction(suite, keylog.SERVER_HANDSHAKE_TRAFFIC_SECRET, keylog.SERVER_TRAFFIC_SECRET_0);
  return records.map((record) => {
    const fromClient = (record.mark === '>') === kizuIsClient;
    return (fromClient ? client : server).note(record.bytes);
  });
}

function keylogOf(lines) {
  const secrets = {};
  for (const line of lines) {
    const [label, , secret] = line.toString().trim().split(' ');
    secrets[label] = Buffer.from(secret, 'hex');
  }
  return secrets;
}

// --- writing a fixture ------------------------------------------------------

const keylogOrder = [
  'CLIENT_HANDSHAKE_TRAFFIC_SECRET',
  'SERVER_HANDSHAKE_TRAFFIC_SECRET',
  'CLIENT_TRAFFIC_SECRET_0',
  'SERVER_TRAFFIC_SECRET_0',
];

function fixtureText(recorded) {
  const lines = [
    '# kizu tls trace 1',
    `# case: ${recorded.name}`,
    `# peer: node ${process.version} OpenSSL ${process.versions.openssl}`,
    `# peer-role: ${recorded.role}`,
    `# recorder: ${recorder} (regenerate: nix develop -c just tls-fixtures)`,
    `# kizu: ${recorded.kizu}`,
    `# seed: ${recorded.seed}`,
    `# suite: ${recorded.suite.name}`,
  ];
  for (const label of keylogOrder) {
    if (recorded.keylog[label]) {
      lines.push(`# keylog: ${label} ${recorded.keylog[label].toString('hex')}`);
    }
  }
  lines.push(`# outcome: ${recorded.outcome}`);
  if (recorded.port !== undefined) {
    lines.push(`# port: ${recorded.port}`);
  }
  const records = recorded.records.map(
    (record, index) => `${record.mark} ${record.bytes.toString('hex')}  ; ${recorded.notes[index]}`,
  );
  const body = `${lines.join('\n')}\n${records.join('\n')}\n`;
  const digest = crypto.createHash('sha256').update(body).digest('hex');
  return `${lines.join('\n')}\n# sha256: ${digest}\n${records.join('\n')}\n`;
}

// --- the cases --------------------------------------------------------------

function expectEqual(what, got, want) {
  if (got !== want) {
    fail(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  }
}

function alertNumber(error) {
  const found = /alert number (\d+)/.exec(error?.message ?? '');
  return found ? Number(found[1]) : null;
}

// clientCase: the Kizu client against a node tls server that answers what it
// reads in upper case and closes.
async function clientCase(kizu, spec) {
  const keylog = [];
  const peer = { handshake: null, read: '', alert: null };
  const server = tls.createServer({ ...tlsOptions, key: pem(`${spec.chain}.key.pem`), cert: pem(`${spec.chain}.pem`) });
  server.on('keylog', (line) => keylog.push(line));
  server.on('tlsClientError', (error) => {
    peer.handshake = 'failed';
    peer.alert = alertNumber(error);
  });
  server.on('secureConnection', (socket) => {
    peer.handshake = 'ok';
    socket.on('data', (chunk) => {
      peer.read += chunk.toString();
      socket.end(chunk.toString().toUpperCase());
    });
    socket.on('error', () => {});
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const middle = relay(() => server.address().port, true);
  const port = await middle.listening;
  const run = kizuRun(kizu, 'client.kizu', [spec.name, String(port), spec.roots, spec.host]);
  const trace = await middle.next();
  const result = await run.done;
  middle.close();
  server.close();
  expectEqual(`${spec.name}: what the Kizu client printed`, result.out, spec.kizuPrints);
  let outcome;
  if (spec.peerAlert === undefined) {
    expectEqual(`${spec.name}: node's handshake`, peer.handshake, 'ok');
    expectEqual(`${spec.name}: what node read`, peer.read, 'hello over tls');
    outcome = `peer handshake ok; peer read "${peer.read}"; peer wrote "${peer.read.toUpperCase()}"`;
  } else {
    expectEqual(`${spec.name}: node's handshake`, peer.handshake, 'failed');
    expectEqual(`${spec.name}: the alert node received`, peer.alert, spec.peerAlert);
    outcome = `peer handshake failed on alert ${alertNames[peer.alert]} (${peer.alert})`;
  }
  return {
    trace,
    keylog,
    outcome,
    role: 'tls server, answers what it reads in upper case',
    kizu: `client host=${spec.host} roots=${spec.roots} now=${now}`,
    kizuIsClient: true,
  };
}

// serverCase: the Kizu server against a node tls client that says
// "hello over tls" and reads the answer.
async function serverCase(kizu, spec) {
  const keylog = [];
  const peer = { handshake: null, read: '', error: null };
  let kizuPort = null;
  let started;
  const ready = new Promise((resolve) => {
    started = resolve;
  });
  const run = kizuRun(kizu, 'server.kizu', [spec.name, spec.chain], (line) => {
    if (kizuPort === null) {
      kizuPort = Number(line);
      started();
    }
  });
  run.done.then(() => started());
  await ready;
  if (kizuPort === null) {
    const result = await run.done;
    fail(`${spec.name}: the Kizu server printed no port\n${result.out}\n${result.err}`);
  }
  const middle = relay(() => kizuPort, false);
  const port = await middle.listening;
  const socket = tls.connect({
    ...tlsOptions,
    host: '127.0.0.1',
    port,
    servername: 'localhost',
    ca: [pem(`${spec.roots}.pem`)],
  });
  socket.on('keylog', (line) => keylog.push(line));
  socket.on('secureConnect', () => {
    peer.handshake = 'ok';
    socket.write('hello over tls');
  });
  socket.on('data', (chunk) => {
    peer.read += chunk.toString();
  });
  socket.on('error', (error) => {
    peer.error = error;
  });
  const trace = await middle.next();
  const result = await run.done;
  middle.close();
  // The first line is the port.
  const printed = result.out.split('\n').slice(1).join('\n');
  expectEqual(`${spec.name}: what the Kizu server printed`, printed, spec.kizuPrints);
  let outcome;
  if (spec.peerError === undefined) {
    expectEqual(`${spec.name}: node's handshake`, peer.handshake, 'ok');
    expectEqual(`${spec.name}: what node read`, peer.read, 'HELLO OVER TLS');
    outcome = `peer handshake ok; peer wrote "hello over tls"; peer read "${peer.read}"`;
  } else {
    expectEqual(`${spec.name}: node's handshake`, peer.handshake, null);
    expectEqual(`${spec.name}: node's error`, peer.error?.code, spec.peerError);
    outcome = `peer handshake failed: ${peer.error.code}`;
  }
  return {
    trace,
    keylog,
    outcome,
    role: `tls client for localhost trusting ${spec.roots}, says "hello over tls"`,
    kizu: `server chain=${spec.chain}`,
    kizuIsClient: false,
  };
}

// httpsClientCase: the Kizu https client against a node https server that
// answers with the path it was asked for.
async function httpsClientCase(kizu, spec) {
  const keylog = [];
  const peer = { handshake: null, alert: null, asked: [] };
  const server = https.createServer(
    { ...tlsOptions, key: pem(`${spec.chain}.key.pem`), cert: pem(`${spec.chain}.pem`) },
    (request, response) => {
      peer.asked.push(`${request.method} ${request.url} Host ${request.headers.host}`);
      response.setHeader('Content-Type', 'text/plain');
      response.end(`hello over tls from ${request.url}`);
    },
  );
  server.on('keylog', (line) => keylog.push(line));
  server.on('secureConnection', () => {
    peer.handshake = 'ok';
  });
  server.on('tlsClientError', (error) => {
    peer.handshake = 'failed';
    peer.alert = alertNumber(error);
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const middle = relay(() => server.address().port, true);
  const port = await middle.listening;
  const run = kizuRun(kizu, 'https_client.kizu', [spec.name, String(port), spec.roots, spec.host]);
  const trace = await middle.next();
  const result = await run.done;
  middle.close();
  server.close();
  expectEqual(`${spec.name}: what the Kizu client printed`, result.out, spec.kizuPrints);
  let outcome;
  if (spec.peerAlert === undefined) {
    expectEqual(`${spec.name}: node's handshake`, peer.handshake, 'ok');
    expectEqual(`${spec.name}: what node was asked`, peer.asked.join('; '), `GET /greeting Host localhost:${port}`);
    outcome = `peer handshake ok; peer served ${peer.asked.join('; ')}`;
  } else {
    expectEqual(`${spec.name}: node's handshake`, peer.handshake, 'failed');
    expectEqual(`${spec.name}: the alert node received`, peer.alert, spec.peerAlert);
    outcome = `peer handshake failed on alert ${alertNames[peer.alert]} (${peer.alert})`;
  }
  return {
    trace,
    keylog,
    outcome,
    // Only a request carries the port, and only a finished handshake sends
    // one; node logs the client's traffic secret the replay opens it with
    // then too.
    port: spec.peerAlert === undefined ? port : undefined,
    role: 'https server, answers with "hello over tls from " and the path',
    kizu: `https client host=${spec.host} roots=${spec.roots} now=real clock`,
    kizuIsClient: true,
  };
}

// httpsServerRun: the Kizu https server of one run against node's https
// clients, one connection after another, each the next fixture. A
// connection's records are taken once both its ends have closed, before the
// next one is opened, so the fixtures are the order the server took them in:
// its handshakes draw from successive splits of the run's seed, and the
// replay has to come in the same order to meet the same bytes.
//
// `connections` lists them: `name` the fixture, `paths` the requests sent
// down it (none for a client that refuses the server), `roots` what the
// client trusts, `keepAlive` whether it asks to keep the connection. A
// kept-alive connection is closed by the client after its last answer.
async function httpsServerRun(kizu, spec) {
  let kizuPort = null;
  let started;
  const ready = new Promise((resolve) => {
    started = resolve;
  });
  const count = spec.connections.reduce((sum, connection) => sum + connection.paths.length, 0);
  const run = kizuRun(kizu, 'https_server.kizu', [spec.name, spec.chain, spec.mode, String(count)], (line) => {
    if (kizuPort === null) {
      kizuPort = Number(line);
      started();
    }
  });
  run.done.then(() => started());
  await ready;
  if (kizuPort === null) {
    const result = await run.done;
    fail(`${spec.name}: the Kizu server printed no port\n${result.out}\n${result.err}`);
  }
  const middle = relay(() => kizuPort, false);
  const port = await middle.listening;
  const fixtures = [];
  for (const [index, connection] of spec.connections.entries()) {
    const keylog = [];
    const agent = new https.Agent({
      ...tlsOptions,
      ca: [pem(`${connection.roots}.pem`)],
      servername: 'localhost',
      keepAlive: connection.keepAlive,
      maxSockets: 1,
    });
    agent.on('keylog', (line) => keylog.push(line));
    const read = [];
    let refused = null;
    for (const route of connection.paths.length > 0 ? connection.paths : ['/']) {
      const got = await new Promise((resolve) => {
        const request = https.request(
          { host: '127.0.0.1', port, path: route, agent, headers: { Host: `localhost:${port}` } },
          (response) => {
            let body = '';
            response.on('data', (chunk) => {
              body += chunk.toString();
            });
            response.on('end', () => resolve({ body: `${response.statusCode} ${body}` }));
          },
        );
        request.on('error', (error) => resolve({ error }));
        request.end();
      });
      if (got.error) {
        refused = got.error;
        break;
      }
      read.push(got.body);
    }
    agent.destroy();
    const trace = await middle.next();
    let outcome;
    if (connection.paths.length > 0) {
      expectEqual(`${connection.name}: node's requests`, refused, null);
      const want = connection.paths.map((route) => `200 hello over tls from ${route}`);
      expectEqual(`${connection.name}: what node read`, read.join('; '), want.join('; '));
      outcome = `peer handshake ok; peer read ${read.map((body) => `"${body}"`).join(', ')}`;
    } else {
      expectEqual(`${connection.name}: node's error`, refused?.code, spec.strangerError);
      outcome = `peer handshake failed: ${refused.code}`;
    }
    const asked = connection.paths.length > 0 ? `asks ${connection.paths.join(' ')}` : 'asks /';
    fixtures.push({
      name: connection.name,
      trace,
      keylog,
      outcome,
      role: `https client for localhost trusting ${connection.roots}, ${connection.keepAlive ? 'keep-alive' : 'no keep-alive'}, ${asked}`,
      kizu: `https server chain=${spec.chain} mode=${spec.mode} run=${spec.name} connection=${index + 1}/${spec.connections.length}`,
      kizuIsClient: false,
    });
  }
  const result = await run.done;
  middle.close();
  // The first line is the port.
  const printed = result.out.split('\n').slice(1).join('\n');
  expectEqual(`${spec.name}: what the Kizu server printed`, printed, spec.kizuPrints);
  return fixtures;
}

const echoed = 'connected with AES-128-GCM\nHELLO OVER TLS\nclosed';
const served = 'served localhost with AES-128-GCM\nhello over tls\nclosed';
const servedRoutes = 'served /one\nserved /two\nserved /three';

const cases = [
  { name: 'client-echo-p256', run: clientCase, chain: 'localhost-p256', roots: 'localhost-p256', host: 'localhost', kizuPrints: echoed },
  { name: 'client-echo-p384', run: clientCase, chain: 'localhost-p384', roots: 'localhost-p384', host: 'localhost', kizuPrints: echoed },
  { name: 'client-echo-rsa', run: clientCase, chain: 'localhost-rsa2048', roots: 'localhost-rsa2048', host: 'localhost', kizuPrints: echoed },
  { name: 'client-wrong-host', run: clientCase, chain: 'localhost-p256', roots: 'localhost-p256', host: 'example.test', kizuPrints: 'NameMismatch', peerAlert: 42 },
  { name: 'client-wrong-root', run: clientCase, chain: 'localhost-p256', roots: 'other-p256', host: 'localhost', kizuPrints: 'UnknownIssuer', peerAlert: 48 },
  { name: 'server-echo-p256', run: serverCase, chain: 'localhost-p256', roots: 'localhost-p256', kizuPrints: served },
  { name: 'server-echo-p384', run: serverCase, chain: 'localhost-p384', roots: 'localhost-p384', kizuPrints: served },
  { name: 'server-echo-rsa', run: serverCase, chain: 'localhost-rsa2048', roots: 'localhost-rsa2048', kizuPrints: served },
  { name: 'server-wrong-root', run: serverCase, chain: 'localhost-p256', roots: 'other-p256', kizuPrints: 'Closed', peerError: 'DEPTH_ZERO_SELF_SIGNED_CERT' },
  { name: 'https-client-fetch', run: httpsClientCase, chain: 'localhost-p256', roots: 'localhost-p256', host: 'localhost', kizuPrints: '200\nhello over tls from /greeting' },
  { name: 'https-client-wrong-root', run: httpsClientCase, chain: 'localhost-p256', roots: 'other-p256', host: 'localhost', kizuPrints: 'UnknownIssuer', peerAlert: 48 },
  { name: 'https-client-wrong-host', run: httpsClientCase, chain: 'localhost-p256', roots: 'localhost-p256', host: '127.0.0.1', kizuPrints: 'NameMismatch', peerAlert: 42 },
  // `accept` takes one request off a connection and closes it after the
  // answer, so each of its connections carries one request. The stranger
  // comes between the others: a server takes a failed handshake silently and
  // waits for the next connection, so with the stranger last the run would
  // wait for a request that never comes.
  {
    name: 'https-server-accept',
    run: httpsServerRun,
    chain: 'localhost-p256',
    mode: 'accept',
    kizuPrints: servedRoutes,
    strangerError: 'DEPTH_ZERO_SELF_SIGNED_CERT',
    connections: [
      { name: 'https-server-accept-one', roots: 'localhost-p256', keepAlive: false, paths: ['/one'] },
      { name: 'https-server-accept-two', roots: 'localhost-p256', keepAlive: false, paths: ['/two'] },
      { name: 'https-server-accept-stranger', roots: 'other-p256', keepAlive: false, paths: [] },
      { name: 'https-server-accept-three', roots: 'localhost-p256', keepAlive: false, paths: ['/three'] },
    ],
  },
  // `first` / `next` keep a connection: /one and /two down one, the stranger,
  // then /three down a fresh one.
  {
    name: 'https-server-loop',
    run: httpsServerRun,
    chain: 'localhost-p256',
    mode: 'loop',
    kizuPrints: servedRoutes,
    strangerError: 'DEPTH_ZERO_SELF_SIGNED_CERT',
    connections: [
      { name: 'https-server-keepalive', roots: 'localhost-p256', keepAlive: true, paths: ['/one', '/two'] },
      { name: 'https-server-stranger', roots: 'other-p256', keepAlive: false, paths: [] },
      { name: 'https-server-fresh', roots: 'localhost-p256', keepAlive: false, paths: ['/three'] },
    ],
  },
];

async function main() {
  if (process.argv.length !== 3) {
    fail(`usage: node ${recorder} <kizu>  (run it as: nix develop -c just tls-fixtures)`);
  }
  if (!process.execPath.startsWith('/nix/store/')) {
    fail(`${process.execPath} is not the devShell's node; run: nix develop -c just tls-fixtures`);
  }
  const kizu = path.resolve(process.argv[2]);
  console.log(`recording with node ${process.versions.node} (OpenSSL ${process.versions.openssl})`);
  const written = [];
  for (const spec of cases) {
    // A case is one Kizu run: one connection, or for an https server run
    // the connections it took, each its own fixture under its own name.
    const ran = await spec.run(kizu, spec);
    for (const got of Array.isArray(ran) ? ran : [{ ...ran, name: spec.name }]) {
      if (got.trace.broken) {
        fail(`${got.name}: ${got.trace.broken}`);
      }
      const suite = suiteOf(got.trace.records);
      const keylog = keylogOf(got.keylog);
      if (got.port !== undefined && !keylog.CLIENT_TRAFFIC_SECRET_0) {
        fail(`${got.name}: node logged no CLIENT_TRAFFIC_SECRET_0 to open the request with`);
      }
      const recorded = {
        ...got,
        seed: seedOf(spec.name),
        suite,
        keylog,
        records: got.trace.records,
        notes: notes(got.trace.records, suite, keylog, got.kizuIsClient),
      };
      written.push({ name: got.name, text: fixtureText(recorded) });
      console.log(`${got.name}: ${got.trace.records.length} records; ${got.outcome}`);
    }
  }
  for (const fixture of written) {
    fs.writeFileSync(path.join(fixtures, `${fixture.name}.trace`), fixture.text);
  }
  console.log(`wrote ${written.length} fixtures to examples/fixtures/tls/`);
}

main().catch((error) => {
  console.error(`record-tls-fixtures: ${error.message}`);
  process.exit(1);
});
