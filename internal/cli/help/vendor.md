Install, update and remove third-party skills. Each is a dependency pinned
to a commit: the manifest records the repository, the directory of the
skill and the commit ([user.dependencies.<name>]), and skenv installs a
copy of the skill at that commit. The copy changes only when "skenv vendor
update" moves the pin, not when the repository moves on. Skills you edit
are different: they are linked from editable git working copies
([user.checkouts.<id>]), which sync pulls.
