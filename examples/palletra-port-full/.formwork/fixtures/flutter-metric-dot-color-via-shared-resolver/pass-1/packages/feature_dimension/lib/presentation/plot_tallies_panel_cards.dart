import 'package:flutter/material.dart';

Color? dotColor(Annotation annotation, AnnotationType type, TieFamilyColors fam) {
  return PartitionWidthColors.outlineForAnnotationOrNull(annotation, type: type, tieFamilyColors: fam);
}
