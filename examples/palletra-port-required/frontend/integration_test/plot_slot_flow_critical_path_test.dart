// Illustrative sample of the slot critical-path integration test so the ported
// rule's scope matches a file in this example tree. It drives the slot wire.
void main() {
  final steps = readSteps('/workflow/steps?slot=$slotKey');
  approveStage(slotKey: slotKey);
}
