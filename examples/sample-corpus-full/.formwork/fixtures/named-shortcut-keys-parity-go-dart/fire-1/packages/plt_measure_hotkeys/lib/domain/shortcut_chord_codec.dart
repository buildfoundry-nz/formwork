class ShortcutChordCodec {
  // Drifted: the Go grammar also allows BracketLeft, but this codec never
  // decodes it, so a rebind to BracketLeft silently unbinds.
  static const _keyAliases = <String, LogicalKeyboardKey>{
    'Delete': LogicalKeyboardKey.delete,
    'ArrowLeft': LogicalKeyboardKey.arrowLeft,
  };
}
