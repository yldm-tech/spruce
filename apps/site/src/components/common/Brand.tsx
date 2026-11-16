// The app's mark and name, as the header and the footer show them.

// first marks the header's, the first image of the page and its largest paint on a phone, which the browser then fetches first.
export function Brand({ first = false }: { first?: boolean }) {
  return (
    <>
      <img src="/logo.png" width="34" height="34" alt="" fetchPriority={first ? "high" : undefined} />
      <strong>Spruce</strong>
    </>
  );
}
