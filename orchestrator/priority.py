"""
Priority Scoring

Scores and prioritizes tasks for execution order.
"""

from dataclasses import dataclass
from typing import List, Optional
from datetime import datetime, timedelta


@dataclass
class ScoredTask:
    """A task with priority score."""
    task_id: str
    title: str
    raw_priority: int  # From ticket (1=urgent, 4=low)
    score: float       # Calculated score (higher = more urgent)
    factors: dict      # Score breakdown


class PriorityScorer:
    """Calculates priority scores for tasks."""

    # Weight factors
    WEIGHTS = {
        'base_priority': 0.4,
        'age': 0.2,
        'complexity': 0.15,
        'dependencies': 0.15,
        'labels': 0.1,
    }

    # Priority labels that boost score
    URGENT_LABELS = {'urgent', 'critical', 'blocker', 'hotfix'}

    def score_task(
        self,
        task_id: str,
        title: str,
        priority: int,
        created_at: Optional[datetime] = None,
        complexity: str = "medium",
        labels: Optional[List[str]] = None,
        blocking_count: int = 0,
    ) -> ScoredTask:
        """
        Calculate priority score for a task.

        Higher score = higher priority.
        """
        factors = {}

        # Base priority score (1-4 maps to 100-25)
        base_score = max(0, (5 - priority) * 25)
        factors['base_priority'] = base_score

        # Age factor (older tasks get slight boost)
        if created_at:
            age_days = (datetime.now() - created_at).days
            age_score = min(100, age_days * 5)  # Max 100 at 20 days
        else:
            age_score = 0
        factors['age'] = age_score

        # Complexity factor (simpler tasks slightly preferred)
        complexity_scores = {'low': 80, 'medium': 50, 'high': 30}
        complexity_score = complexity_scores.get(complexity, 50)
        factors['complexity'] = complexity_score

        # Dependencies (tasks blocking others get priority)
        dependency_score = min(100, blocking_count * 20)
        factors['dependencies'] = dependency_score

        # Label boost
        label_score = 0
        if labels:
            label_set = set(l.lower() for l in labels)
            if label_set & self.URGENT_LABELS:
                label_score = 100
        factors['labels'] = label_score

        # Calculate weighted score
        total_score = sum(
            factors[k] * self.WEIGHTS[k]
            for k in self.WEIGHTS
        )

        return ScoredTask(
            task_id=task_id,
            title=title,
            raw_priority=priority,
            score=round(total_score, 2),
            factors=factors,
        )

    def rank_tasks(self, tasks: List[ScoredTask]) -> List[ScoredTask]:
        """Rank tasks by score (highest first)."""
        return sorted(tasks, key=lambda t: t.score, reverse=True)


def score_tasks(tasks_data: List[dict]) -> List[dict]:
    """
    Entry point for Go orchestrator.

    Args:
        tasks_data: List of task dictionaries

    Returns:
        List of scored tasks sorted by priority
    """
    scorer = PriorityScorer()
    scored = []

    for task in tasks_data:
        created_at = None
        if task.get('created_at'):
            created_at = datetime.fromisoformat(task['created_at'])

        scored_task = scorer.score_task(
            task_id=task.get('id', ''),
            title=task.get('title', ''),
            priority=task.get('priority', 4),
            created_at=created_at,
            complexity=task.get('complexity', 'medium'),
            labels=task.get('labels', []),
            blocking_count=task.get('blocking_count', 0),
        )

        scored.append({
            'task_id': scored_task.task_id,
            'title': scored_task.title,
            'raw_priority': scored_task.raw_priority,
            'score': scored_task.score,
            'factors': scored_task.factors,
        })

    return sorted(scored, key=lambda t: t['score'], reverse=True)


if __name__ == "__main__":
    # Test with sample tasks
    sample_tasks = [
        {
            'id': 'TASK-1',
            'title': 'Fix login bug',
            'priority': 1,
            'labels': ['critical', 'bug'],
            'complexity': 'low',
        },
        {
            'id': 'TASK-2',
            'title': 'Add user profile page',
            'priority': 3,
            'labels': ['feature'],
            'complexity': 'medium',
        },
        {
            'id': 'TASK-3',
            'title': 'Refactor database layer',
            'priority': 4,
            'labels': ['tech-debt'],
            'complexity': 'high',
            'blocking_count': 3,
        },
    ]

    results = score_tasks(sample_tasks)
    for task in results:
        print(f"{task['task_id']}: {task['title']} - Score: {task['score']}")
