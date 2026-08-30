// want: no-hand-rolled-member-tile-secondary-scope
import 'package:flutter/material.dart';
import 'package:plt_proto/team.pb.dart' show CrewSummary;

class CrewMemberRow extends StatelessWidget {
  const CrewMemberRow({super.key, required this.member});
  final CrewSummary member;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHigh.withValues(alpha: 0.35),
        borderRadius: ShellSpacing.cornerRadiusSm,
      ),
      child: Padding(
        padding: ShellSpacing.gapLg,
        child: Text(member.displayName),
      ),
    );
  }
}
