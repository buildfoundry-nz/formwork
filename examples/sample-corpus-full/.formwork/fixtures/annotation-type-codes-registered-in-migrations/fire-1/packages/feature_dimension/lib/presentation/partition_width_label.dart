// The partition-width canvas label names an annotation-type code the registry never
// seeds ('n'), so the badge branch is dead on disk (sweep-8 #5).
const String kPartitionWidthSegmentTypeCode = 'n';

bool isPartitionWidth(String markerTypeCode) => markerTypeCode == 'n';
