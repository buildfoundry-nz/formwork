class IdentificationRepository {
  // BAD: hand-rolled parallel switch on a canonical segment_code. The keys
  // drift from the plural codes Go emits and the re-detect button vanishes.
  String? detectorForSectionTag(String code) {
    switch (code) {
      case 'external_partitions':
        return '/api/detection/external-partitions';
    }
    return null;
  }
}
