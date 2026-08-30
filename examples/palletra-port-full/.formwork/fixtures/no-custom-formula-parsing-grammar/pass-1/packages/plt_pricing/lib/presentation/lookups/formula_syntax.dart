import 'package:plt_pricing/formula_rules_provider.dart';

// Grammar is consumed from the server, not mirrored. A single incidental use of
// one function name (e.g. 'ceil' in a doc example) is fine — the offender
// signature needs all four names co-occurring.
class FormulaNotation {
  FormulaNotation(this.ref);
  final Ref ref;

  Set<String> get unaryFns => ref.watch(formulaRulesProvider).unaryFunctions;

  String highlight(String token) => unaryFns.contains(token) ? 'fn' : 'ident';
}
