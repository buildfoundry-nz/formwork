// Legacy no-slot path: lists steps by page type, never references the slot wire.
void main() {
  final steps = readSteps('/workflow/steps');
  approveStage(pageKind: pageKind);
}
