ImpactedLineState fromWire(String w) {
  if (w == 'AFFECTED_LINE_STATE_FORMULA_MISSING') return ImpactedLineState.formulaAbsent; // want: single-source-line-state-wire-mapper
  return ImpactedLineState.unknown;
}
