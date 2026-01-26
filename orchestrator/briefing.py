"""
Daily Brief Generator

Generates daily status briefs for team communication.
"""

from dataclasses import dataclass
from typing import List, Optional
from datetime import datetime, timedelta


@dataclass
class TaskSummary:
    """Summary of a task for the brief."""
    id: str
    title: str
    status: str
    pr_url: Optional[str] = None
    completed_at: Optional[datetime] = None
    error: Optional[str] = None


@dataclass
class DailyBrief:
    """Daily status brief."""
    date: datetime
    completed: List[TaskSummary]
    in_progress: List[TaskSummary]
    failed: List[TaskSummary]
    upcoming: List[TaskSummary]
    stats: dict


class BriefGenerator:
    """Generates daily status briefs."""

    def generate(
        self,
        tasks: List[TaskSummary],
        date: Optional[datetime] = None,
    ) -> DailyBrief:
        """Generate a daily brief from task list."""
        date = date or datetime.now()

        # Categorize tasks
        completed = [t for t in tasks if t.status == 'completed']
        in_progress = [t for t in tasks if t.status == 'running']
        failed = [t for t in tasks if t.status == 'failed']
        upcoming = [t for t in tasks if t.status == 'pending']

        # Calculate stats
        stats = {
            'total': len(tasks),
            'completed_count': len(completed),
            'in_progress_count': len(in_progress),
            'failed_count': len(failed),
            'upcoming_count': len(upcoming),
            'success_rate': (
                len(completed) / (len(completed) + len(failed)) * 100
                if (len(completed) + len(failed)) > 0
                else 0
            ),
        }

        return DailyBrief(
            date=date,
            completed=completed,
            in_progress=in_progress,
            failed=failed,
            upcoming=upcoming,
            stats=stats,
        )

    def to_slack_message(self, brief: DailyBrief) -> str:
        """Format brief as Slack message."""
        date_str = brief.date.strftime('%B %d, %Y')

        msg = f"*🤖 Pilot Daily Brief - {date_str}*\n\n"

        # Stats summary
        msg += f"📊 *Summary*\n"
        msg += f"• Completed: {brief.stats['completed_count']}\n"
        msg += f"• In Progress: {brief.stats['in_progress_count']}\n"
        msg += f"• Failed: {brief.stats['failed_count']}\n"
        msg += f"• Upcoming: {brief.stats['upcoming_count']}\n"
        msg += f"• Success Rate: {brief.stats['success_rate']:.0f}%\n\n"

        # Completed tasks
        if brief.completed:
            msg += "✅ *Completed*\n"
            for task in brief.completed[:5]:
                pr_link = f" (<{task.pr_url}|PR>)" if task.pr_url else ""
                msg += f"• `{task.id}` {task.title}{pr_link}\n"
            if len(brief.completed) > 5:
                msg += f"  _...and {len(brief.completed) - 5} more_\n"
            msg += "\n"

        # In progress
        if brief.in_progress:
            msg += "⏳ *In Progress*\n"
            for task in brief.in_progress[:5]:
                msg += f"• `{task.id}` {task.title}\n"
            msg += "\n"

        # Failed tasks
        if brief.failed:
            msg += "❌ *Failed (needs attention)*\n"
            for task in brief.failed[:3]:
                error = f": {task.error[:50]}..." if task.error else ""
                msg += f"• `{task.id}` {task.title}{error}\n"
            msg += "\n"

        # Upcoming
        if brief.upcoming:
            msg += "📋 *Next Up*\n"
            for task in brief.upcoming[:3]:
                msg += f"• `{task.id}` {task.title}\n"

        return msg

    def to_markdown(self, brief: DailyBrief) -> str:
        """Format brief as Markdown."""
        date_str = brief.date.strftime('%B %d, %Y')

        md = f"# Pilot Daily Brief - {date_str}\n\n"

        # Stats
        md += "## Summary\n\n"
        md += f"| Metric | Count |\n"
        md += f"|--------|-------|\n"
        md += f"| Completed | {brief.stats['completed_count']} |\n"
        md += f"| In Progress | {brief.stats['in_progress_count']} |\n"
        md += f"| Failed | {brief.stats['failed_count']} |\n"
        md += f"| Upcoming | {brief.stats['upcoming_count']} |\n"
        md += f"| Success Rate | {brief.stats['success_rate']:.0f}% |\n\n"

        # Completed
        if brief.completed:
            md += "## ✅ Completed\n\n"
            for task in brief.completed:
                pr_link = f" - [PR]({task.pr_url})" if task.pr_url else ""
                md += f"- **{task.id}**: {task.title}{pr_link}\n"
            md += "\n"

        # In Progress
        if brief.in_progress:
            md += "## ⏳ In Progress\n\n"
            for task in brief.in_progress:
                md += f"- **{task.id}**: {task.title}\n"
            md += "\n"

        # Failed
        if brief.failed:
            md += "## ❌ Failed\n\n"
            for task in brief.failed:
                md += f"- **{task.id}**: {task.title}\n"
                if task.error:
                    md += f"  - Error: {task.error}\n"
            md += "\n"

        # Upcoming
        if brief.upcoming:
            md += "## 📋 Upcoming\n\n"
            for task in brief.upcoming:
                md += f"- **{task.id}**: {task.title}\n"

        return md


def generate_brief(tasks_data: List[dict], format: str = "slack") -> str:
    """
    Entry point for Go orchestrator.

    Args:
        tasks_data: List of task dictionaries
        format: Output format ("slack" or "markdown")

    Returns:
        Formatted brief string
    """
    tasks = [
        TaskSummary(
            id=t.get('id', ''),
            title=t.get('title', ''),
            status=t.get('status', 'pending'),
            pr_url=t.get('pr_url'),
            error=t.get('error'),
        )
        for t in tasks_data
    ]

    generator = BriefGenerator()
    brief = generator.generate(tasks)

    if format == "markdown":
        return generator.to_markdown(brief)
    return generator.to_slack_message(brief)


if __name__ == "__main__":
    # Test with sample data
    sample_tasks = [
        {'id': 'TASK-1', 'title': 'Add login', 'status': 'completed', 'pr_url': 'https://github.com/org/repo/pull/1'},
        {'id': 'TASK-2', 'title': 'Fix bug', 'status': 'completed', 'pr_url': 'https://github.com/org/repo/pull/2'},
        {'id': 'TASK-3', 'title': 'Add auth', 'status': 'running'},
        {'id': 'TASK-4', 'title': 'Refactor', 'status': 'failed', 'error': 'Tests failing'},
        {'id': 'TASK-5', 'title': 'Add profile', 'status': 'pending'},
        {'id': 'TASK-6', 'title': 'Add settings', 'status': 'pending'},
    ]

    print("=== SLACK FORMAT ===")
    print(generate_brief(sample_tasks, "slack"))
    print("\n=== MARKDOWN FORMAT ===")
    print(generate_brief(sample_tasks, "markdown"))
