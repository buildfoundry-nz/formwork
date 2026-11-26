// A re-introduced grammar mirror: the single-arg function names hardcoded as a
// compact Set literal (the canonical shape) instead of read from the server.
const unaryFns = <String>{'ceil', 'floor', 'round', 'abs'}; // want: no-custom-formula-parsing-grammar

String highlight(String token) =>
    unaryFns.contains(token) ? 'fn' : 'ident';
