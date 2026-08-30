import '../../../../shared/generated/dart/lib/palletra/domain/v1/job.pb.dart';

// GOOD: consume the generated proto class; no hand-rolled fromJson / annotation.
String jobTitle(Job job) => job.title;
