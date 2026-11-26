// The value-branch anti-pattern: a literal dock-status comparison.
bool isUnassigned(job) {
  return job.dockStatus == 'unslotted'; // want: flutter-no-hardcoded-dock-status
}
