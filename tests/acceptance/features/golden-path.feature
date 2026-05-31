Feature: Keeping an LLM agent on the project's golden path

  When an agent proposes a command we'd rather it not run, we turn it away and
  point it at the project's preferred way of doing the job. This is a nudge, not
  a lock: it keeps a cooperative agent on the rails, but anyone determined to go
  around it still can.

  Background:
    Given the project asks agents to use:
      | instead of running | use this  | because                                        |
      | go test            | just test | tests run through the task runner              |
      | git tag            |           | releases go through the pipeline (Versionator) |

  Rule: A discouraged command is turned away, with a reason

    Scenario: Running the tests the direct way
      When the agent runs "go test ./..."
      Then the command is turned away
      And the agent is told "tests run through the task runner"
      And the agent is pointed at "just test"

    Scenario: Tagging a release by hand
      When the agent runs "git tag v1.4.0"
      Then the command is turned away
      And the agent is told "releases go through the pipeline (Versionator)"

    Scenario: An ordinary command is left alone
      When the agent runs "ls -la && cat README.md"
      Then the command is allowed

  Rule: Phrasing the same command differently does not get it through

    Scenario Outline: The discouraged command, dressed up another way
      When the agent runs "<command>"
      Then the command is turned away

      Examples:
        | command                  |
        | bash -c 'go test'        |
        | task=test; go $task      |
        | CMD='go test'; eval $CMD |

  Rule: We point the way; we do not stand guard

    Scenario: A command whose meaning is only settled when it runs is left alone
      When the agent runs "eval $RESOLVED_AT_RUNTIME"
      Then the command is allowed
