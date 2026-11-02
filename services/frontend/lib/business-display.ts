/** The business mark follows the quoted trading name, then the full name. */
export function businessMark(name: string): string {
  const quoted = name.match(/[«“"]([^»”"]+)[»”"]/u)?.[1];
  return (quoted ?? name).match(/\p{L}/u)?.[0].toUpperCase() ?? '?';
}

/** Keep the street and house in compact navigation; the profile keeps the full address. */
export function shortBusinessAddress(address: string): string {
  const parts = address
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean);
  const house = /^(?:д\.?\s*)?\d[\dа-яa-z/\s.-]*$/iu;
  const street =
    /^(?:ул\.?|улица|проспект|пр-т|пер\.?|переулок|шоссе|наб\.?|набережная|бульвар|площадь)\s/iu;
  let start = parts.findIndex((part) => street.test(part));
  if (start < 0) start = parts.length > 2 && house.test(parts[2]) ? 1 : 0;
  return [parts[start], house.test(parts[start + 1] ?? '') ? parts[start + 1] : '']
    .filter(Boolean)
    .join(', ');
}
