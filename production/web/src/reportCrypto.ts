import { tr } from "./i18n";
/** RFC 7516 compact JWE, RSA-OAEP-256 + A256GCM. No plaintext download fallback. */
export type ReportPublicKey = {
  kty: "RSA";
  n: string;
  e: string;
  alg: "RSA-OAEP-256";
  use: "enc";
  kid: string;
};
function b64url(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}
async function validatedRSA(key: ReportPublicKey): Promise<CryptoKey> {
  if (!globalThis.crypto?.subtle) throw new Error(tr("m3f017eeccd"));
  if (
    !key ||
    key.kty !== "RSA" ||
    key.alg !== "RSA-OAEP-256" ||
    key.use !== "enc" ||
    key.e !== "AQAB" ||
    !/^[-_A-Za-z0-9]{512}$/.test(key.n) ||
    !/^[-_A-Za-z0-9]{43}$/.test(key.kid)
  )
    throw new Error(tr("mc005b5586f"));
  const encoder = new TextEncoder();
  const thumbprint = b64url(
    new Uint8Array(
      await crypto.subtle.digest(
        "SHA-256",
        encoder.encode(JSON.stringify({ e: key.e, kty: key.kty, n: key.n })),
      ),
    ),
  );
  if (thumbprint !== key.kid) throw new Error(tr("mfc19dc712a"));
  const rsa = await crypto.subtle.importKey(
    "jwk",
    { kty: key.kty, n: key.n, e: key.e, alg: key.alg, ext: true },
    { name: "RSA-OAEP", hash: "SHA-256" },
    false,
    ["wrapKey"],
  );
  if ((rsa.algorithm as RsaHashedKeyAlgorithm).modulusLength !== 3072)
    throw new Error(tr("mf3294ab0f4"));
  return rsa;
}
export async function validateReportPublicKey(
  key: ReportPublicKey,
): Promise<void> {
  await validatedRSA(key);
}
export async function encryptReport(
  report: unknown,
  key: ReportPublicKey,
): Promise<string> {
  const rsa = await validatedRSA(key);
  const encoder = new TextEncoder();
  const plaintext = encoder.encode(JSON.stringify(report));
  if (plaintext.byteLength > 2 * 1024 * 1024)
    throw new Error(tr("mbf37679e0e"));
  const aes = await crypto.subtle.generateKey(
    { name: "AES-GCM", length: 256 },
    true,
    ["encrypt"],
  );
  const header = b64url(
    encoder.encode(
      JSON.stringify({
        alg: "RSA-OAEP-256",
        enc: "A256GCM",
        kid: key.kid,
        typ: "gma-bug-report+jwe",
        cty: "application/json",
      }),
    ),
  );
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const encrypted = new Uint8Array(
    await crypto.subtle.encrypt(
      {
        name: "AES-GCM",
        iv,
        additionalData: encoder.encode(header),
        tagLength: 128,
      },
      aes,
      plaintext,
    ),
  );
  const wrapped = await crypto.subtle.wrapKey("raw", aes, rsa, {
    name: "RSA-OAEP",
  });
  return [
    header,
    b64url(new Uint8Array(wrapped)),
    b64url(iv),
    b64url(encrypted.slice(0, -16)),
    b64url(encrypted.slice(-16)),
  ].join(".");
}
