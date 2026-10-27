export function cx(
  ...values: Array<string | number | false | null | undefined>
): string {
  return values.filter(Boolean).join(' ')
}
