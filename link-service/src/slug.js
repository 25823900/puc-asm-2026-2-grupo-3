import { randomInt } from 'node:crypto';

const ALPHABET = '0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ';

export function createSlug() {
  return Array.from({ length: 7 }, () => ALPHABET[randomInt(ALPHABET.length)]).join('');
}
