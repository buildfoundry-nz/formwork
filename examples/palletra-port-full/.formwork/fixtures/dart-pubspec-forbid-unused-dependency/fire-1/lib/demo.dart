import 'package:collection/collection.dart';

int firstOrZero(List<int> xs) => xs.firstWhereOrNull((e) => e > 0) ?? 0;
