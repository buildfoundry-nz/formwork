// A hand-map of the dock-status vocabulary on the FE — the #1469 drift.
String dockStatusLabel(String status) {
  switch (status) {
    case 'unslotted': // want: flutter-no-hardcoded-dock-status
      return 'Unassigned';
    case 'ready_for_audit': // want: flutter-no-hardcoded-dock-status
      return 'Ready for QA';
    default:
      return '';
  }
}
