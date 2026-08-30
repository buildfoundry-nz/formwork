import 'package:collection/collection.dart';
import 'package:intl/intl.dart';

int firstOrZero(List<int> xs) => xs.firstWhereOrNull((e) => e > 0) ?? 0;

String today() => DateFormat.yMMMd().format(DateTime.now());
