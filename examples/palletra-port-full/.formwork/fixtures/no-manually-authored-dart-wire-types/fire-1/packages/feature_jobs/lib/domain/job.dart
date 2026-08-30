import 'package:json_annotation/json_annotation.dart';

// BAD: hand-rolled wire type mirroring the proto instead of importing the
// generated class.
@JsonSerializable() // want: no-manually-authored-dart-wire-types
class Job {
  Job(this.id, this.title);

  final String id;
  final String title;
}
