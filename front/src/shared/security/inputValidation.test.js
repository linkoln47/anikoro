import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  parseMalUsername,
  parseSyncJobId,
  validateAccountUsername,
  validateEmail,
  validateMalUsername,
  validatePassword,
  validateSyncJobId,
} from './inputValidation.js'

describe('MAL username validation', () => {
  it('accepts and normalizes safe MAL usernames', () => {
    assert.equal(parseMalUsername(' user_name-42 '), 'user_name-42')
    assert.equal(parseMalUsername('ＡＢＣ_１２'), 'ABC_12')
  })

  it('rejects malicious or malformed username payloads', () => {
    const payloads = [
      '',
      ' ',
      'a',
      '<script>alert(1)</script>',
      '<ScRiPt src=//evil.example/x.js></ScRiPt>',
      '"><img src=x onerror=alert(1)>',
      "'><svg/onload=alert(1)>",
      '<iframe srcdoc="<script>alert(1)</script>">',
      '&lt;script&gt;alert(1)&lt;/script&gt;',
      'javascript:alert(1)',
      'data:text/html,<script>alert(1)</script>',
      '../../etc/passwd',
      '..\\..\\windows\\system32',
      '/admin',
      '\\admin',
      'user/name',
      'user?x=1',
      'user#hash',
      'user&role=admin',
      'user=name',
      '%2Fadmin',
      '%252Fadmin',
      '..%2F..%2Fetc%2Fpasswd',
      'http://evil.example',
      'https://evil.example/@me',
      '//evil.example/path',
      'user@example.com',
      'user.name',
      'user name',
      'user\tname',
      'user\nname',
      'user\r\nX-Injected: yes',
      'null\u0000byte',
      '${alert(1)}',
      '{{constructor.constructor("alert(1)")()}}',
      '`touch /tmp/pwned`',
      '$(curl evil.example)',
      ';DROP TABLE users;',
      "' OR '1'='1",
      'admin/*comment*/',
      '<!--comment-->',
      '[]',
      '{}',
      '()',
      '👾',
      'ユーザー',
      'user\u200Bname',
      'user\u202Ename',
      '＜script＞alert(1)＜/script＞',
      'veryveryverylongveryveryverylongx',
    ]

    for (const payload of payloads) {
      assert.equal(validateMalUsername(payload).ok, false, payload)
      assert.throws(() => parseMalUsername(payload), Error, payload)
    }
  })
})

describe('sync job id validation', () => {
  it('accepts backend-generated URL-safe sync job ids', () => {
    const jobId = 'AbC123_-AbC123_-AbC123_-'

    assert.equal(parseSyncJobId(jobId), jobId)
  })

  it('rejects malicious or malformed sync job id payloads', () => {
    const payloads = [
      '',
      ' ',
      'short',
      'AbC123_-AbC123_-AbC123_',
      'AbC123_-AbC123_-AbC123_-x',
      'aaaaaaaaaaaaaaaaaaaaaaa.',
      'aaaaaaaaaaaaaaaaaaaaaa/a',
      'aaaaaaaaaaaaaaaaaaaaaa?a',
      'aaaaaaaaaaaaaaaaaaaaaa#a',
      'aaaaaaaaaaaaaaaaaaaaaa%a',
      '../../etc/passwd',
      '..\\..\\windows\\system32',
      '%2Fadmin',
      '%252Fadmin',
      '<script>alert(1)</script>',
      '"><img src=x onerror=alert(1)>',
      'javascript:alert(1)',
      'user\r\nX-Injected: yes',
      'null\u0000byte',
      'ＡＢＣ123_-AbC123_-AbC12',
      'AbC123_-AbC123_-AbC12👾',
    ]

    for (const payload of payloads) {
      assert.equal(validateSyncJobId(payload).ok, false, payload)
      assert.throws(() => parseSyncJobId(payload), Error, payload)
    }
  })
})

describe('account email validation', () => {
  it('accepts and normalizes valid emails', () => {
    assert.deepEqual(validateEmail('  User@Example.COM '), {
      ok: true,
      value: 'user@example.com',
      error: '',
    })
  })

  it('rejects malformed emails', () => {
    const payloads = [
      '',
      ' ',
      'plainaddress',
      'user@example',
      'user@@example.com',
      'user @example.com',
      'user@exam ple.com',
      'user\nname@example.com',
      `${'a'.repeat(250)}@example.com`,
    ]

    for (const payload of payloads) {
      assert.equal(validateEmail(payload).ok, false, payload)
    }
  })
})

describe('account username validation', () => {
  it('accepts safe usernames', () => {
    assert.equal(validateAccountUsername(' Alice_99 ').value, 'Alice_99')
  })

  it('rejects malformed usernames', () => {
    const payloads = ['', 'a', 'bad name', 'bad@name', 'user/name', 'a'.repeat(33)]

    for (const payload of payloads) {
      assert.equal(validateAccountUsername(payload).ok, false, payload)
    }
  })
})

describe('account password validation', () => {
  it('accepts passwords within the byte bounds', () => {
    assert.equal(validatePassword('supersecret').ok, true)
  })

  it('rejects too short or too long passwords', () => {
    assert.equal(validatePassword('short').ok, false)
    assert.equal(validatePassword('a'.repeat(73)).ok, false)
    // Multi-byte characters count by UTF-8 byte length.
    assert.equal(validatePassword('💥'.repeat(19)).ok, false)
  })
})
