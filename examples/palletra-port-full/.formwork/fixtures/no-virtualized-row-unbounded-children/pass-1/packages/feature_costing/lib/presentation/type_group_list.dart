import 'package:flutter/material.dart';

class CategoryGroupList extends StatelessWidget {
  const CategoryGroupList({super.key, required this.rows});

  final List<ClusterRow> rows;

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      itemCount: rows.length,
      itemBuilder: (context, index) {
        final row = rows[index];
        return EntryRow(row);
      },
    );
  }
}
