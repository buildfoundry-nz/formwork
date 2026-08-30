class ContinueGate {
  // Reads next_pending off the primary-action response (#24), no second scan.
  bool canProceed(MainActionResponse response) {
    return response.upcomingUnapproved.pageId.isEmpty;
  }
}
