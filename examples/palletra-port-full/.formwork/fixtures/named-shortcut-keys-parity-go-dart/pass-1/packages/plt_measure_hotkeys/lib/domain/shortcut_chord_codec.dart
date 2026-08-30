class ShortcutChordCodec {
  // In lockstep with the Go grammar: every allowedShortcutKeys token has a codec
  // entry here.
  static const _keyAliases = <String, LogicalKeyboardKey>{
    'Delete': LogicalKeyboardKey.delete,
    'ArrowLeft': LogicalKeyboardKey.arrowLeft,
    'BracketLeft': LogicalKeyboardKey.bracketLeft,
  };
}
