import 'package:flutter/material.dart';

class CategoryGroupList extends StatelessWidget {
  const CategoryGroupList({super.key, required this.groups});

  final List<CategoryGroup> groups;

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      itemCount: groups.length,
      itemBuilder: (context, index) {
        final group = groups[index];
        return ExpansionTile(
          title: Text(group.label),
          children: <Widget>[ // want: no-virtualized-row-unbounded-children
            for (final instance in group.instances) EntryRow(instance),
          ],
        );
      },
    );
  }
}
