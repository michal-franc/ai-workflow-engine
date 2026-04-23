---
title: "API Reference"
order: 2
---

## Endpoints

All endpoints are scoped under `/p/<project-slug>/`.

### Issues

| Method | Path                | Description           |
|:-------|:--------------------|:----------------------|
| GET    | `/`                 | List view             |
| GET    | `/board`            | Board view            |
| GET    | `/issue/<slug>`     | Issue detail          |
| POST   | `/issue/<slug>`     | Update issue metadata |

### Comments

| Method | Path                           | Description    |
|:-------|:-------------------------------|:---------------|
| GET    | `/issue/<slug>/comments`       | List comments  |
| POST   | `/issue/<slug>/comments`       | Add comment    |
| POST   | `/issue/<slug>/comments/toggle` | Toggle done   |
| POST   | `/issue/<slug>/comments/delete` | Delete comment |

### Update Payload

```json
{
  "status": "in progress",
  "priority": "high",
  "version": "1.0",
  "assignee": "my-bot",
  "labels": ["bug", "urgent"],
  "body": "Updated markdown body"
}
```

All fields are optional. Only provided fields are updated.
