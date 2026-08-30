// Drives the canonical-workflow slot critical path.
void main() {
  final steps = readSteps('/workflow/steps?slot=$slotKey');
  approveStage(slotKey: slotKey);
}
