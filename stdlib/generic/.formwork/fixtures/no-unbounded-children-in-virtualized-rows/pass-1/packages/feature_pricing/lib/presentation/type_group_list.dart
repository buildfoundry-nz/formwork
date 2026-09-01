import 'package:flutter/material.dart';

class TypeGroupList extends StatelessWidget {
  const TypeGroupList({super.key, required this.rows});

  final List<GroupRow> rows;

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      itemCount: rows.length,
      itemBuilder: (context, index) {
        final row = rows[index];
        return InstanceRow(row);
      },
    );
  }
}
