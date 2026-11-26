void main() {
  final cell = DeviceMatrixCell.fromEnvironment(); // want: cross-browser-specs-must-not-read-matrix-cell
  runMeasureJourney();
}
