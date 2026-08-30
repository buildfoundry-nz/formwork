class IdentificationRepository {
  // GOOD: read the server-provided WorkflowPagePanel fields; never branch on
  // the segment_code literal. Passing the code through as DATA is fine too.
  String? identifierFor(WorkflowPagePanel section) {
    final probe = WorkflowPagePanel(sectionCode: 'external_partitions');
    return section.identificationEndpoint ?? probe.identificationEndpoint;
  }
}
