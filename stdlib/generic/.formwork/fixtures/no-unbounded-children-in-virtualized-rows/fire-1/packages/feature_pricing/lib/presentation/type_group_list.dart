import 'package:flutter/material.dart';

class TypeGroupList extends StatelessWidget {
  const TypeGroupList({super.key, required this.groups});

  final List<TypeGroup> groups;

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      itemCount: groups.length,
      itemBuilder: (context, index) {
        final group = groups[index];
        return ExpansionTile(
          title: Text(group.label),
          children: <Widget>[ // want: no-unbounded-children-in-virtualized-rows
            for (final instance in group.instances) InstanceRow(instance),
          ],
        );
      },
    );
  }
}
