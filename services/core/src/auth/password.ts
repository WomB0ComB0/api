import { randomBytes, scrypt as scryptCb, timingSafeEqual } from 'node:crypto';
import { promisify } from 'node:util';

const scrypt = promisify(scryptCb);

export async function hashPassword(password: string): Promise<string> {
  if (typeof Bun !== 'undefined') {
    return Bun.password.hash(password, {
      algorithm: 'argon2id',
      memoryCost: 65536,
      timeCost: 2,
    });
  }

  const salt = randomBytes(16).toString('hex');
  const derived = (await scrypt(password, salt, 64)) as Buffer;
  return `scrypt$${salt}$${derived.toString('hex')}`;
}

export async function verifyPassword(password: string, storedHash: string): Promise<boolean> {
  if (storedHash.startsWith('scrypt$')) {
    const [, salt, hashHex] = storedHash.split('$');
    if (!salt || !hashHex) {
      return false;
    }
    const derived = (await scrypt(password, salt, 64)) as Buffer;
    const expected = Buffer.from(hashHex, 'hex');
    if (derived.length !== expected.length) {
      return false;
    }
    return timingSafeEqual(derived, expected);
  }

  if (typeof Bun !== 'undefined') {
    return Bun.password.verify(password, storedHash);
  }

  return false;
}
