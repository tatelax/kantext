X Need to fix checking who is modifying what task
X Change number of tasks passing or failing to use a filled circle to show what percent of tasks passed or failed as a visual indicator
* Make logo
X Rename "TODO" to "Inbox"
X Make Inbox/todo, in progress, and done impossible to remove. If they dont exist, the web server should add them
X Add a pencil icon to rename custom columns but the default ones should not be able to be deleted. It should match the style of the delete column button including how that button handles overriding the draggable header so the user can click it instead of dragging the column
X Make task view wider
X Lower opacity on blockers behind task view and dialog boxes
X Add red exclamation mark to the left of task titles (or left of the icon indicating the task has acceptance criteria) to stale tasks. Define what stale means in config.json. By default it will be tasks that have not been updated in more than 1 week
X Add config menu to modify config.json from UI
X Add strikethru on tasks in done column
X If a task fails to pass ask the user if they want to move it to in progress
* Fix height of columns to match padding between bottom of screen and left of screen
X Detect if the test output is in JSON. If so, display the JSON in a nice way in the test results screen.
X Add an encircled '?' tooltip to the right side of various settings options to explain what they do. Use custom CSS so the tooltips match the theme. The tooltips should appear when the user hovers over the ?.
X Make it so when a test is running, the circle showing number of successful tests is empty and the counter says -/x
X Change it so that instead of an hourglass when a test is running we show a loading spinner
X Fix the spacing between the task title and task metadata so that the spacing is the same whether the task has tests associated with it or not. Currently if the task doesnt have a test associated with it, it has less spacing than ones that do. Also investigate the placement of the play button. It is not aligned vertically with the task title.
X Ensure README.md in Kantext is up to date. Ensure setup instructions are correct for someone who may not have any prerequisites installed. Provide instructions for Linux, macOS, and Windows.
X Ensure we aren't marking tasks as stale if they're in the Done column. Marking tasks as stale only happens for tasks not in the Done column.
X Fix the issue where moving tasks to a different column isn't updating the 'updated_at' date for that task
* Animate cards moving around as you drag another card between them. Currently it is working correctly that cards create a space where the user can drop a dragged card in between. However the cards instantly move out of the way. It would be nice if they smoothly animated out of the way instead. Similar to how Trello works.
X Go through the code base and remove any unnecessary comments that explain things that don't need explaining.