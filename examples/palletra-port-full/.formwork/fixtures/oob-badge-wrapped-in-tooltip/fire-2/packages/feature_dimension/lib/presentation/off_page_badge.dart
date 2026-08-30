class OffPageBadge {
  Widget build(BuildContext context, OffPageFlag outOfGauge) {
    return Text(outOfGauge.message);
  }
}

// COMMENT-IMMUNITY PROOF (code-only-dart). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: code-only-dart` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// return Tooltip(message: outOfGauge.message, child: const Icon(Icons.warning));
