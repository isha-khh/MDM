/**
 * Apple Plist XML parser — converts plist XML to JavaScript objects.
 */

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type PlistValue = string | number | boolean | Date | Uint8Array | PlistValue[] | { [key: string]: PlistValue };

export function parsePlist(xml: string): PlistValue | null {
  try {
    const parser = new DOMParser();
    const doc = parser.parseFromString(xml, "text/xml");
    // DOMParser reports XML parse failures by injecting a <parsererror>
    // element rather than throwing. Treat that as untrusted input we should
    // refuse — defence-in-depth even though downstream only reads textContent.
    if (doc.querySelector("parsererror")) return null;
    const plist = doc.querySelector("plist");
    if (!plist || !plist.firstElementChild) return null;
    return parseNode(plist.firstElementChild);
  } catch {
    return null;
  }
}

function parseNode(node: Element): PlistValue {
  switch (node.tagName) {
    case "dict":
      return parseDict(node);
    case "array":
      return parseArray(node);
    case "string":
      return node.textContent || "";
    case "integer":
      return parseInt(node.textContent || "0", 10);
    case "real":
      return parseFloat(node.textContent || "0");
    case "true":
      return true;
    case "false":
      return false;
    case "date":
      return new Date(node.textContent || "");
    case "data":
      return node.textContent || "";
    default:
      return node.textContent || "";
  }
}

function parseDict(node: Element): { [key: string]: PlistValue } {
  const result: { [key: string]: PlistValue } = {};
  const children = Array.from(node.children);
  for (let i = 0; i < children.length; i += 2) {
    if (children[i].tagName === "key" && children[i + 1]) {
      const key = children[i].textContent || "";
      result[key] = parseNode(children[i + 1]);
    }
  }
  return result;
}

function parseArray(node: Element): PlistValue[] {
  return Array.from(node.children).map(parseNode);
}

/**
 * Decode base64 string to UTF-8 text (handles Chinese/multibyte characters).
 * atob() only handles Latin1, so we need TextDecoder for proper UTF-8.
 */
export function base64ToUtf8(base64: string): string {
  const binaryString = atob(base64);
  const bytes = new Uint8Array(binaryString.length);
  for (let i = 0; i < binaryString.length; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }
  return new TextDecoder("utf-8").decode(bytes);
}

/**
 * Try to decode base64 plist and parse it.
 */
export function decodeAndParsePlist(base64: string): PlistValue | null {
  try {
    const xml = base64ToUtf8(base64);
    return parsePlist(xml);
  } catch {
    return null;
  }
}

/**
 * Try to decode base64 to XML string.
 */
export function decodeBase64(base64: string): string | null {
  try {
    return base64ToUtf8(base64);
  } catch {
    return null;
  }
}
