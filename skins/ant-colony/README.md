# Ant Colony

The forge as a cross-section of a nest. Grass runs along the title line, and soil strata darken below. Panes are chambers dug out of the earth and spaces are side chambers off the main shaft. Every member of the crew is an ant. Whatever is busy leaves an amber scent trail, and the footer's honey bar stands for the colony's stores. It suggests the spaces in the rail and styles them on top too. Dark.

Both modes: dark is the nest underground; light is a sunlit, sandy formicarium (same chambers, ants and scent trail).

| State | Shown as |
| --- | --- |
| working | an amber ant that scurries and leaves a glowing scent trail; its chamber pulses with pheromone light |
| blocked / error | a red ant waving for help; its chamber is ringed in red |
| done | a green ant; its chamber is edged in moss |
| idle | a brown ant at rest |

## The animated scene

Ant Colony carries a scene (`scene/`, PixiJS, about 36 KB of code), so the nest is alive behind the panes. Tunnels run round every chamber, down the gutters and up to the surface. The granary in the header holds the queen and the colony's growing store.

| State | What the ants do |
| --- | --- |
| working | workers carry crumbs from the chamber to the granary along a glowing scent trail |
| blocked / error | ants crowd round the chamber and patrol it under a red alarm ring, and a runner dashes to the granary |
| done | two ants carry a seed home, then one dozes by the chamber |
| idle (or any other word) | two ants amble round the chamber and doze in the gutter |
| running | treated as working |
| on another tab | foragers bring crumbs in along the surface |
| always | the nest crew digs and carries soil up through the header, and nurses tend the queen, so the colony never looks empty |

The scene is off until you allow it: **Allow scene…** in the Themes panel, or `la skin install --allow-code`. It runs sandboxed, with no network and no access to your terminals. Under reduced motion it draws a still picture. With the scene off, the stylesheet draws the nest on its own.

    la skin install ./skins/ant-colony
    la skin use ant-colony    # or pick "Skin: Ant Colony" in ⌘K

System faces only (Avenir Next / Futura, with fallbacks). Apache-2.0.
