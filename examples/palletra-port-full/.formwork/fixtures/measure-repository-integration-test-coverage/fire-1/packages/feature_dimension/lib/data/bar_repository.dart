// Bar wire repository. Its protojson<->proto round-trip + ImpactedPage patch
// contract has NO integration test naming it, so it can silently drift (#6479).
class BeamRepository {
  Future<void> save() async {}
}
