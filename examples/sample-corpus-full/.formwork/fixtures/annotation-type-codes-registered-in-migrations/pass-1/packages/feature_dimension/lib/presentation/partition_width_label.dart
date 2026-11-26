// The partition-width canvas label names the real registry code, so the badge
// branch matches a seeded marker_types row.
const String kPartitionWidthSegmentTypeCode = 'partition_width_segment';

bool isPartitionWidth(String markerTypeCode) =>
    markerTypeCode == 'partition_width_segment';
