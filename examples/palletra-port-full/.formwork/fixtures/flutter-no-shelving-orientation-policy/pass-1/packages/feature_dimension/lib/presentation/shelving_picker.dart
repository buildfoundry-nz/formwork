import 'package:feature_dimension/data/shelving_types_provider.dart';

bool displayOrientationControl(ShelvingType type) {
  return type.allowsOrientation;
}
